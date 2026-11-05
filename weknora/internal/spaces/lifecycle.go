package spaces

import (
	"context"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

// Archiving freezes collaborative content without removing its readers.
// Ownership and membership administration remain available for handover.
type SpaceState struct {
	SpaceID   string `gorm:"primaryKey;type:varchar(36)"`
	Archived  bool
	UpdatedAt time.Time
}

func (SpaceState) TableName() string { return "solex_space_state" }
func (s *Service) archived(ctx context.Context, db *gorm.DB, id string) bool {
	var state SpaceState
	return db.WithContext(ctx).First(&state, "space_id=?", id).Error == nil && state.Archived
}
func (s *Service) requireWritableSpace(ctx context.Context, db *gorm.DB, id string) error {
	if s.archived(ctx, db, id) {
		return fmt.Errorf("%w: 空间已归档，请先恢复空间", ErrConflict)
	}
	return nil
}
func (s *Service) Archive(ctx context.Context, id string, archived bool) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, e := s.member(ctx, tx, id, u.ID, 4); e != nil {
			return e
		}
		row := SpaceState{SpaceID: id, Archived: archived, UpdatedAt: time.Now().UTC()}
		if e := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; e != nil {
			return e
		}
		action := "space.reopened"
		if archived {
			action = "space.archived"
		}
		return s.event(ctx, tx, id, u.ID, action, id)
	})
}

// Administrators recover collaboration ownership only after an account has
// been disabled. This endpoint never returns private documents or chat data.
type HandoverSpace struct {
	Space
	OwnerName string `json:"owner_name"`
}

func (s *Service) OrphanedSpaces(ctx context.Context) ([]HandoverSpace, error) {
	u, e := s.actor(ctx)
	if e != nil || !u.IsSystemAdmin {
		return nil, ErrForbidden
	}
	rows := []HandoverSpace{}
	e = s.db.WithContext(ctx).Table("organizations o").Select("o.id,o.name,o.owner_id,u.username AS owner_name").Joins("JOIN users u ON u.id=o.owner_id").Where("o.deleted_at IS NULL AND (NOT u.is_active OR u.deleted_at IS NOT NULL)").Scan(&rows).Error
	return rows, e
}
func (s *Service) AdminHandover(ctx context.Context, id, target string) error {
	u, e := s.actor(ctx)
	if e != nil || !u.IsSystemAdmin {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var org types.Organization
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&org, "id=?", id).Error; e != nil {
			return ErrNotFound
		}
		var old types.User
		if e := tx.Unscoped().First(&old, "id=?", org.OwnerID).Error; e != nil {
			return e
		}
		if old.IsActive && !old.DeletedAt.Valid {
			return fmt.Errorf("%w: 有效账号应自行移交空间", ErrInvalid)
		}
		var next types.User
		if e := tx.First(&next, "id=? AND is_active=?", target, true).Error; e != nil {
			return ErrNotFound
		}
		if e := tx.Model(&types.SpaceMember{}).Where("space_id=? AND role='owner'", id).Update("role", "admin").Error; e != nil {
			return e
		}
		m := types.SpaceMember{SpaceID: id, UserID: target, Role: "owner", JoinedAt: time.Now().UTC()}
		if e := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "user_id"}}, DoUpdates: clause.Assignments(map[string]any{"role": "owner"})}).Create(&m).Error; e != nil {
			return e
		}
		if e := tx.Model(&org).Updates(map[string]any{"owner_id": target, "owner_tenant_id": next.TenantID, "updated_at": time.Now().UTC()}).Error; e != nil {
			return e
		}
		return s.event(ctx, tx, id, u.ID, "space.admin_handover", target)
	})
}

func (s *Service) RequireAccountDeletionReady(ctx context.Context, id string) error {
	var n int64
	if e := s.db.WithContext(ctx).Model(&types.Organization{}).Where("owner_id=?", id).Count(&n).Error; e != nil {
		return e
	}
	if n > 0 {
		return fmt.Errorf("%w: 请先移交该账号的共享空间", ErrConflict)
	}
	if e := s.db.WithContext(ctx).Model(&types.SpaceResource{}).Where("owner_user_id=?", id).Count(&n).Error; e != nil {
		return e
	}
	if n > 0 {
		return fmt.Errorf("%w: 请先处理该账号的个人资源；可先停用账号", ErrConflict)
	}
	return nil
}

func (s *Service) PendingInvitations(ctx context.Context, id string) ([]types.SpaceInvitation, error) {
	u, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = s.member(ctx, s.db, id, u.ID, 3); e != nil {
		return nil, e
	}
	rows := []types.SpaceInvitation{}
	e = s.db.WithContext(ctx).Where("space_id=? AND status='pending' AND expires_at>?", id, time.Now()).Find(&rows).Error
	return rows, e
}
func (s *Service) CancelInvitation(ctx context.Context, id string) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row types.SpaceInvitation
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id=? AND status='pending'", id).Error; e != nil {
			return ErrNotFound
		}
		if !strings.EqualFold(row.Email, u.Email) {
			if _, e := s.member(ctx, tx, row.SpaceID, u.ID, 3); e != nil {
				return e
			}
		}
		return tx.Model(&row).Update("status", "revoked").Error
	})
}

// Ready prevents enabling account-sharing against an unconverted database.
func (s *Service) Ready(ctx context.Context) bool {
	var n int64
	return s.db.WithContext(ctx).Table("solex_space_migration_state").Where("id=1 AND completed_at IS NOT NULL").Count(&n).Error == nil && n == 1
}

type SharedHandover struct {
	Kind           string `json:"kind"`
	ResourceID     string `json:"resource_id"`
	SourceTenantID uint64 `json:"source_tenant_id"`
	SpaceID        string `json:"space_id"`
	SpaceName      string `json:"space_name"`
	OwnerName      string `json:"owner_name"`
	Name           string `json:"name"`
}

func (s *Service) OrphanedSharedResources(ctx context.Context) ([]SharedHandover, error) {
	u, e := s.actor(ctx)
	if e != nil || !u.IsSystemAdmin {
		return nil, ErrForbidden
	}
	rows := []SharedHandover{}
	e = s.db.WithContext(ctx).Table("solex_space_resources r").Select("r.kind,r.resource_id,r.source_tenant_id,g.space_id,o.name AS space_name,u.username AS owner_name").Joins("JOIN users u ON u.id=r.owner_user_id AND (NOT u.is_active OR u.deleted_at IS NOT NULL)").Joins("JOIN solex_space_grants g ON g.kind=r.kind AND g.resource_id=r.resource_id AND g.source_tenant_id=r.source_tenant_id").Joins("JOIN organizations o ON o.id=g.space_id AND o.deleted_at IS NULL").Scan(&rows).Error
	if e != nil {
		return nil, e
	}
	result := []SharedHandover{}
	for _, r := range rows {
		if name, e := s.resourceName(ctx, r.Kind, r.ResourceID, r.SourceTenantID); e == nil {
			r.Name = name
			result = append(result, r)
		}
	}
	return result, nil
}
func (s *Service) AdminResourceHandover(ctx context.Context, kind, id string, tenant uint64, target string) error {
	u, e := s.actor(ctx)
	if e != nil || !u.IsSystemAdmin {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockResource(ctx, tx, kind, id, tenant); e != nil {
			return e
		}
		var r types.SpaceResource
		if e := tx.First(&r, "kind=? AND resource_id=? AND source_tenant_id=?", kind, id, tenant).Error; e != nil {
			return ErrNotFound
		}
		var owner types.User
		if r.OwnerUserID == "" {
			return ErrInvalid
		}
		if e := tx.Unscoped().First(&owner, "id=?", r.OwnerUserID).Error; e != nil {
			return ErrNotFound
		}
		if owner.IsActive && !owner.DeletedAt.Valid {
			return ErrForbidden
		}
		// Recovery may retain this team's existing shared asset, never expose a
		// previously private resource or grant it to an unrelated team.
		var grant types.SpaceGrant
		if e := tx.First(&grant, "kind=? AND resource_id=? AND source_tenant_id=? AND space_id=?", kind, id, tenant, target).Error; e != nil {
			return ErrForbidden
		}
		var org types.Organization
		if e := tx.First(&org, "id=?", target).Error; e != nil {
			return ErrNotFound
		}
		if kind == "agent" {
			if e := New(tx).validateAgentPublication(ctx, target, id, tenant); e != nil {
				return e
			}
		}
		if e := tx.Model(&r).Updates(map[string]any{"owner_user_id": "", "owner_space_id": target, "created_at": time.Now().UTC()}).Error; e != nil {
			return e
		}
		return s.event(ctx, tx, target, u.ID, "resource.admin_handover", kind+":"+id)
	})
}
