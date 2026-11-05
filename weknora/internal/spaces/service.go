// Package spaces owns account-based collaboration authorization. A personal
// tenant is a storage boundary, not a grant of access to transferred resources.
package spaces

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrForbidden = errors.New("无权执行此操作")
	ErrConflict  = errors.New("内容或权限已发生变化，请刷新后重试")
	ErrInvalid   = errors.New("参数不符合共享空间规则")
	ErrNotFound  = errors.New("资源不存在或不可访问")
)

type Service struct{ db *gorm.DB }

var installed atomic.Pointer[Service]

func New(db *gorm.DB) *Service { return &Service{db: db} }

// Install is called once during application assembly. Tests may install their
// own isolated database; enabling the feature without a policy fails closed.
func Install(s *Service) { installed.Store(s) }
func Current() (*Service, error) {
	s := installed.Load()
	if s == nil {
		return nil, ErrForbidden
	}
	return s, nil
}

func RoleLevel(role string) int {
	switch role {
	case "owner":
		return 4
	case "admin":
		return 3
	case "editor":
		return 2
	case "viewer":
		return 1
	}
	return 0
}

type Source struct {
	SpaceID       string    `json:"space_id"`
	SpaceName     string    `json:"space_name"`
	Role          string    `json:"role"`
	AllowEdit     bool      `json:"allow_edit"`
	AllowDownload bool      `json:"allow_download"`
	HistoryFrom   time.Time `json:"history_from"`
}
type Access struct {
	Read     bool     `json:"read"`
	Edit     bool     `json:"edit"`
	Download bool     `json:"download"`
	Manage   bool     `json:"manage"`
	Sources  []Source `json:"sources"`
}

func (s *Service) actor(ctx context.Context) (*types.User, error) {
	c := types.CallerFromContext(ctx)
	if c.UserID == "" || c.TenantID == 0 {
		return nil, ErrForbidden
	}
	var u types.User
	if err := s.db.WithContext(ctx).Where("id = ? AND is_active = ?", c.UserID, true).First(&u).Error; err != nil {
		return nil, ErrForbidden
	}
	// Users never gain another person's grants by selecting their tenant header.
	if u.TenantID != c.TenantID {
		return nil, ErrForbidden
	}
	return &u, nil
}

func (s *Service) member(ctx context.Context, db *gorm.DB, space, user string, minimum int) (*types.SpaceMember, error) {
	var m types.SpaceMember
	err := db.WithContext(ctx).Joins("JOIN organizations o ON o.id = solex_space_members.space_id AND o.deleted_at IS NULL").
		Where("solex_space_members.space_id = ? AND solex_space_members.user_id = ?", space, user).First(&m).Error
	if err != nil || RoleLevel(m.Role) < minimum {
		return nil, ErrForbidden
	}
	return &m, nil
}

func (s *Service) Check(ctx context.Context, kind, id string, tenant uint64) (Access, error) {
	if _, e := s.resourceName(ctx, kind, id, tenant); e != nil {
		return Access{}, e
	}
	return s.checkAccess(ctx, kind, id, tenant)
}
func (s *Service) checkAccess(ctx context.Context, kind, id string, tenant uint64) (Access, error) {
	a := Access{Sources: []Source{}}
	u, err := s.actor(ctx)
	if err != nil {
		return a, err
	}
	var owner types.SpaceResource
	err = s.db.WithContext(ctx).Where("kind = ? AND resource_id = ? AND source_tenant_id = ?", kind, id, tenant).First(&owner).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Only genuinely personal, unmigrated resources may use their original
		// storage owner. Registered space ownership always takes precedence.
		if tenant == u.TenantID {
			return Access{Read: true, Edit: true, Download: true, Manage: true, Sources: []Source{}}, nil
		}
	} else if err != nil {
		return a, err
	}
	if owner.OwnerUserID == u.ID {
		return Access{Read: true, Edit: true, Download: true, Manage: true, Sources: []Source{}}, nil
	}
	if owner.OwnerSpaceID != "" {
		if m, e := s.member(ctx, s.db, owner.OwnerSpaceID, u.ID, 1); e == nil {
			a.Read, a.Download = true, true
			a.Edit, a.Manage = RoleLevel(m.Role) >= 2 && !s.archived(ctx, s.db, owner.OwnerSpaceID), RoleLevel(m.Role) >= 3 && !s.archived(ctx, s.db, owner.OwnerSpaceID)
			var org types.Organization
			if e := s.db.WithContext(ctx).First(&org, "id=?", owner.OwnerSpaceID).Error; e != nil {
				return a, e
			}
			a.Sources = append(a.Sources, Source{SpaceID: owner.OwnerSpaceID, SpaceName: org.Name, Role: m.Role, AllowEdit: a.Edit, AllowDownload: true, HistoryFrom: owner.CreatedAt})
		}
	}
	var rows []Source
	err = s.db.WithContext(ctx).Table("solex_space_grants g").
		Select("g.space_id, o.name AS space_name, m.role, g.allow_edit, g.allow_download, g.history_from").
		Joins("JOIN solex_space_members m ON m.space_id = g.space_id AND m.user_id = ?", u.ID).
		Joins("JOIN organizations o ON o.id = g.space_id AND o.deleted_at IS NULL").
		Where("g.kind = ? AND g.resource_id = ? AND g.source_tenant_id = ?", kind, id, tenant).Scan(&rows).Error
	if err != nil {
		return Access{}, err
	}
	for _, row := range rows {
		if RoleLevel(row.Role) == 0 {
			continue
		}
		a.Read = true
		row.AllowEdit = row.AllowEdit && RoleLevel(row.Role) >= 2 && !s.archived(ctx, s.db, row.SpaceID)
		a.Edit = a.Edit || row.AllowEdit
		a.Download = a.Download || row.AllowDownload
		a.Sources = append(a.Sources, row)
	}
	return a, nil
}

type Space struct {
	Archived    bool   `json:"archived"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerID     string `json:"owner_id"`
	Role        string `json:"my_role" gorm:"column:role"`
	MemberCount int64  `json:"member_count"`
}

func (s *Service) List(ctx context.Context) ([]Space, error) {
	u, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	rows := []Space{}
	err = s.db.WithContext(ctx).Table("organizations o").
		Select("o.id, o.name, o.description, o.owner_id, m.role, COALESCE(st.archived,FALSE) AS archived, (SELECT COUNT(*) FROM solex_space_members sm WHERE sm.space_id=o.id) AS member_count").
		Joins("JOIN solex_space_members m ON m.space_id=o.id AND m.user_id=?", u.ID).
		Joins("LEFT JOIN solex_space_state st ON st.space_id=o.id").
		Where("o.deleted_at IS NULL").Order("o.created_at DESC").Scan(&rows).Error
	return rows, err
}

func (s *Service) event(ctx context.Context, tx *gorm.DB, space, actor, action, subject string) error {
	return tx.WithContext(ctx).Create(&types.SpaceEvent{SpaceID: space, ActorID: actor, Action: action, Subject: subject, CreatedAt: time.Now().UTC()}).Error
}

func (s *Service) Create(ctx context.Context, name, description string) (*types.Organization, error) {
	u, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 || len([]rune(description)) > 2000 {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	o := &types.Organization{ID: uuid.NewString(), Name: name, Description: description, OwnerID: u.ID, OwnerTenantID: u.TenantID, InviteCode: strings.ReplaceAll(uuid.NewString(), "-", ""), InviteCodeValidityDays: 7, MemberLimit: 200, CreatedAt: now, UpdatedAt: now}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(o).Error; e != nil {
			return e
		}
		if e := tx.Create(&types.SpaceMember{SpaceID: o.ID, UserID: u.ID, Role: "owner", JoinedAt: now}).Error; e != nil {
			return e
		}
		return s.event(ctx, tx, o.ID, u.ID, "space.created", name)
	})
	return o, err
}

type Member struct {
	types.SpaceMember
	Username string `json:"username"`
	Active   bool   `json:"active"`
}

func (s *Service) Members(ctx context.Context, space string) ([]Member, error) {
	u, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = s.member(ctx, s.db, space, u.ID, 1); e != nil {
		return nil, e
	}
	rows := []Member{}
	e = s.db.WithContext(ctx).Table("solex_space_members m").Select("m.*,u.username,u.is_active AS active").Joins("JOIN users u ON u.id=m.user_id").Where("m.space_id=?", space).Order("m.joined_at").Scan(&rows).Error
	return rows, e
}

func (s *Service) Invite(ctx context.Context, space, email string) (*types.SpaceInvitation, error) {
	u, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 255 || !strings.Contains(email, "@") {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	i := &types.SpaceInvitation{ID: uuid.NewString(), SpaceID: space, Email: email, InvitedBy: u.ID, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)}
	e = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.member(ctx, tx, space, u.ID, 3); err != nil {
			return err
		}
		if e := s.requireWritableSpace(ctx, tx, space); e != nil {
			return e
		}
		// Supersede an earlier invitation; it cannot be accepted twice.
		if err := tx.Model(&types.SpaceInvitation{}).Where("space_id=? AND email=? AND status=?", space, email, "pending").Update("status", "revoked").Error; err != nil {
			return err
		}
		if err := tx.Create(i).Error; err != nil {
			return err
		}
		return s.event(ctx, tx, space, u.ID, "member.invited", email)
	})
	return i, e
}

func (s *Service) Invitations(ctx context.Context) ([]types.SpaceInvitation, error) {
	u, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	rows := []types.SpaceInvitation{}
	e = s.db.WithContext(ctx).Where("LOWER(email)=? AND status=? AND expires_at>?", strings.ToLower(u.Email), "pending", time.Now()).Find(&rows).Error
	for i := range rows {
		var org types.Organization
		if e := s.db.WithContext(ctx).First(&org, "id=?", rows[i].SpaceID).Error; e == nil {
			rows[i].SpaceName = org.Name
		}
	}
	return rows, e
}

func (s *Service) Accept(ctx context.Context, id string) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var i types.SpaceInvitation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND LOWER(email)=? AND status=? AND expires_at>?", id, strings.ToLower(u.Email), "pending", time.Now()).First(&i).Error; err != nil {
			return ErrNotFound
		}
		var org types.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&org, "id=?", i.SpaceID).Error; err != nil {
			return ErrNotFound
		}
		if e := s.requireWritableSpace(ctx, tx, i.SpaceID); e != nil {
			return e
		}
		var n int64
		if err := tx.Model(&types.SpaceMember{}).Where("space_id=?", i.SpaceID).Count(&n).Error; err != nil {
			return err
		}
		var existing int64
		tx.Model(&types.SpaceMember{}).Where("space_id=? AND user_id=?", i.SpaceID, u.ID).Count(&existing)
		if existing == 0 && org.MemberLimit > 0 && n >= int64(org.MemberLimit) {
			return fmt.Errorf("%w: 空间成员已达上限", ErrInvalid)
		}
		now := time.Now().UTC()
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.SpaceMember{SpaceID: i.SpaceID, UserID: u.ID, Role: "viewer", JoinedAt: now}).Error; err != nil {
			return err
		}
		r := tx.Model(&types.SpaceInvitation{}).Where("id=? AND status=?", id, "pending").Updates(map[string]any{"status": "accepted", "accepted_at": now})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrConflict
		}
		return s.event(ctx, tx, i.SpaceID, u.ID, "member.joined", u.ID)
	})
}

// SetMemberRole also implements leaving (empty role). Administrators cannot
// appoint peers or change another administrator; ownership transfer is separate.
func (s *Service) SetMemberRole(ctx context.Context, space, user, role string) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	if role != "" && (RoleLevel(role) == 0 || role == "owner") {
		return ErrInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := s.member(ctx, tx, space, u.ID, 1)
		if err != nil {
			return err
		}
		target, err := s.member(ctx, tx, space, user, 1)
		if err != nil {
			return err
		}
		if target.Role == "owner" {
			return fmt.Errorf("%w: 所有者须先转交空间", ErrInvalid)
		}
		selfLeave := user == u.ID && role == ""
		if !selfLeave && (RoleLevel(actor.Role) < 3 || (actor.Role != "owner" && (target.Role == "admin" || role == "admin"))) {
			return ErrForbidden
		}
		q := tx.Where("space_id=? AND user_id=?", space, user)
		if role == "" {
			err = q.Delete(&types.SpaceMember{}).Error
		} else {
			err = q.Model(&types.SpaceMember{}).Update("role", role).Error
		}
		if err != nil {
			return err
		}
		return s.event(ctx, tx, space, u.ID, "member.role_changed", user+":"+role)
	})
}

func (s *Service) TransferSpace(ctx context.Context, space, target string) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	if u.ID == target {
		return ErrInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var org types.Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&org, "id=?", space).Error; err != nil {
			return ErrNotFound
		}
		if org.OwnerID != u.ID {
			return ErrForbidden
		}
		if _, err := s.member(ctx, tx, space, target, 1); err != nil {
			return err
		}
		var next types.User
		if err := tx.First(&next, "id=? AND is_active=?", target, true).Error; err != nil {
			return ErrNotFound
		}
		if err := tx.Model(&types.SpaceMember{}).Where("space_id=? AND user_id=?", space, u.ID).Update("role", "admin").Error; err != nil {
			return err
		}
		if err := tx.Model(&types.SpaceMember{}).Where("space_id=? AND user_id=?", space, target).Update("role", "owner").Error; err != nil {
			return err
		}
		if err := tx.Model(&types.Organization{}).Where("id=?", space).Updates(map[string]any{"owner_id": target, "owner_tenant_id": next.TenantID, "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		return s.event(ctx, tx, space, u.ID, "space.transferred", target)
	})
}

func (s *Service) Delete(ctx context.Context, space string) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m, err := s.member(ctx, tx, space, u.ID, 4)
		if err != nil || m.Role != "owner" {
			return ErrForbidden
		}
		var n int64
		if err = tx.Model(&types.SpaceResource{}).Where("owner_space_id=?", space).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: 请先移交或归档空间自有资源", ErrInvalid)
		}
		if err = tx.Where("space_id=?", space).Delete(&types.SpaceGrant{}).Error; err != nil {
			return err
		}
		if err = tx.Where("id=?", space).Delete(&types.Organization{}).Error; err != nil {
			return err
		}
		return s.event(ctx, tx, space, u.ID, "space.deleted", space)
	})
}

// ValidateActor enforces the fixed personal execution scope for HTTP callers.
func (s *Service) ValidateActor(ctx context.Context) error { _, err := s.actor(ctx); return err }
