package spaces

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Resource struct {
	Kind           string            `json:"kind"`
	ID             string            `json:"id"`
	SourceTenantID uint64            `json:"source_tenant_id"`
	Name           string            `json:"name"`
	Owned          bool              `json:"owned"`
	Access         Access            `json:"access"`
	Grant          *types.SpaceGrant `json:"grant,omitempty"`
}

func (s *Service) resourceName(ctx context.Context, kind, id string, tenant uint64) (string, error) {
	switch kind {
	case "knowledge_base":
		var kb types.KnowledgeBase
		if e := s.db.WithContext(ctx).First(&kb, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return "", ErrNotFound
			}
			return "", e
		}
		return kb.Name, nil
	case "agent":
		var a types.CustomAgent
		if types.IsBuiltinAgentID(id) {
			return "", ErrInvalid
		}
		if e := s.db.WithContext(ctx).First(&a, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return "", ErrNotFound
			}
			return "", e
		}
		return a.Name, nil
	default:
		return "", ErrInvalid
	}
}

func (s *Service) Resources(ctx context.Context, space string) ([]Resource, error) {
	u, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = s.member(ctx, s.db, space, u.ID, 1); e != nil {
		return nil, e
	}
	var grants []types.SpaceGrant
	if e = s.db.WithContext(ctx).Where("space_id=?", space).Find(&grants).Error; e != nil {
		return nil, e
	}
	var owned []types.SpaceResource
	if e = s.db.WithContext(ctx).Where("owner_space_id=?", space).Find(&owned).Error; e != nil {
		return nil, e
	}
	result := []Resource{}
	seen := map[string]bool{}
	add := func(kind, id string, tenant uint64, isOwned bool, g *types.SpaceGrant) {
		key := fmt.Sprintf("%s/%s/%d", kind, id, tenant)
		if seen[key] {
			return
		}
		name, err := s.resourceName(ctx, kind, id, tenant)
		if err != nil {
			return
		}
		a, err := s.Check(ctx, kind, id, tenant)
		if err != nil || !a.Read {
			return
		}
		result = append(result, Resource{Kind: kind, ID: id, SourceTenantID: tenant, Name: name, Owned: isOwned, Access: a, Grant: g})
		seen[key] = true
	}
	for _, o := range owned {
		add(o.Kind, o.ResourceID, o.SourceTenantID, true, nil)
	}
	for i := range grants {
		g := &grants[i]
		add(g.Kind, g.ResourceID, g.SourceTenantID, false, g)
	}
	return result, nil
}

// Share requires source ownership AND publishing rights in the target space.
// Receivers cannot grant more than the source owner chose, including admins.
func (s *Service) Share(ctx context.Context, space, kind, id string, tenant uint64, edit, download bool) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	if _, e = s.resourceName(ctx, kind, id, tenant); e != nil {
		return e
	}
	a, e := s.Check(ctx, kind, id, tenant)
	if e != nil || !a.Manage {
		return ErrForbidden
	}
	if _, e = s.member(ctx, s.db, space, u.ID, 2); e != nil {
		return e
	}
	if e = s.requireWritableSpace(ctx, s.db, space); e != nil {
		return e
	}
	if kind == "agent" {
		if e = s.validateAgentPublication(ctx, space, id, tenant); e != nil {
			return e
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.member(ctx, tx, space, u.ID, 2); err != nil {
			return err
		}
		if err := lockResource(ctx, tx, kind, id, tenant); err != nil {
			return err
		}
		if err := New(tx).Require(ctx, kind, id, tenant, "manage"); err != nil {
			return err
		}
		target := space
		if e := New(tx).requireWritableSpace(ctx, tx, target); e != nil {
			return e
		}
		if kind == "agent" {
			if e := New(tx).validateAgentPublication(ctx, target, id, tenant); e != nil {
				return e
			}
		}
		var previous types.SpaceGrant
		err := tx.Where("space_id=? AND kind=? AND resource_id=? AND source_tenant_id=?", space, kind, id, tenant).First(&previous).Error
		now := time.Now().UTC()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			previous = types.SpaceGrant{ID: uuid.NewString(), SpaceID: space, Kind: kind, ResourceID: id, SourceTenantID: tenant, SharedBy: u.ID, AllowEdit: edit, AllowDownload: download, HistoryFrom: now, CreatedAt: now, UpdatedAt: now}
			err = tx.Create(&previous).Error
		} else if err == nil {
			err = tx.Model(&previous).Updates(map[string]any{"allow_edit": edit, "allow_download": download, "updated_at": now}).Error
		}
		if err != nil {
			return err
		}
		return s.event(ctx, tx, space, u.ID, "resource.shared", fmt.Sprintf("%s:%s edit=%t download=%t", kind, id, edit, download))
	})
}

func (s *Service) Unshare(ctx context.Context, space, kind, id string, tenant uint64) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	a, e := s.Check(ctx, kind, id, tenant)
	if e != nil {
		return e
	}
	if !a.Manage {
		if _, e = s.member(ctx, s.db, space, u.ID, 3); e != nil {
			return e
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockResource(ctx, tx, kind, id, tenant); e != nil {
			return e
		}
		current, e := New(tx).Check(ctx, kind, id, tenant)
		if e != nil {
			return e
		}
		if !current.Manage {
			if _, e = s.member(ctx, tx, space, u.ID, 3); e != nil {
				return e
			}
		}

		if e := tx.Where("space_id=? AND kind=? AND resource_id=? AND source_tenant_id=?", space, kind, id, tenant).Delete(&types.SpaceGrant{}).Error; e != nil {
			return e
		}
		return s.event(ctx, tx, space, u.ID, "resource.unshared", kind+":"+id)
	})
}

func (s *Service) validateAgentPublication(ctx context.Context, space, id string, tenant uint64) error {
	var agent types.CustomAgent
	if e := s.db.WithContext(ctx).First(&agent, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
		return ErrNotFound
	}
	return s.validateAgentConfig(ctx, space, agent.Config)
}

func (s *Service) validateAgentConfig(ctx context.Context, space string, config types.CustomAgentConfig) error {
	if config.KBSelectionMode == "all" || config.KBSelectionMode == "" {
		return fmt.Errorf("%w: 共享智能体必须明确选择知识库或不使用知识库", ErrInvalid)
	}
	if config.KBSelectionMode == "none" {
		return nil
	}
	if config.KBSelectionMode != "selected" {
		return ErrInvalid
	}
	for _, kb := range config.KnowledgeBases {
		if _, _, _, e := s.ResourceScope(ctx, "knowledge_base", kb, 0); e != nil {
			return fmt.Errorf("%w: 依赖知识库不可用", ErrInvalid)
		}
		var n int64
		if e := s.db.WithContext(ctx).Model(&types.SpaceGrant{}).Where("space_id=? AND kind=? AND resource_id=?", space, "knowledge_base", kb).Count(&n).Error; e != nil {
			return e
		}
		if n == 0 {
			if e := s.db.WithContext(ctx).Model(&types.SpaceResource{}).Where("owner_space_id=? AND kind=? AND resource_id=?", space, "knowledge_base", kb).Count(&n).Error; e != nil {
				return e
			}
		}
		if n == 0 {
			return fmt.Errorf("%w: 知识库 %s 尚未向此空间授权", ErrInvalid, kb)
		}
	}
	return nil
}

func (s *Service) Agent(ctx context.Context, id string, tenant uint64) (*types.CustomAgent, error) {
	a, e := s.Check(ctx, "agent", id, tenant)
	if e != nil || !a.Read {
		return nil, ErrForbidden
	}
	var agent types.CustomAgent
	if e = s.db.WithContext(ctx).First(&agent, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
		return nil, ErrNotFound
	}
	if !s.IsPersonalOwner(ctx, "agent", id, tenant) {
		if agent.Config.KBSelectionMode == "all" || agent.Config.KBSelectionMode == "" {
			return nil, fmt.Errorf("%w: 请所有者明确共享智能体的知识库清单", ErrInvalid)
		}
		if agent.Config.KBSelectionMode == "selected" {
			for _, id := range agent.Config.KnowledgeBases {
				var kb types.KnowledgeBase
				if e = s.db.WithContext(ctx).First(&kb, "id=?", id).Error; e != nil {
					return nil, ErrForbidden
				}
				ka, err := s.Check(ctx, "knowledge_base", id, kb.TenantID)
				if err != nil || !ka.Read {
					return nil, fmt.Errorf("%w: 智能体依赖的知识库授权已失效", ErrForbidden)
				}
			}
		}
	}
	return &agent, nil
}

// Transfer changes logical ownership atomically. Physical paths and indexes
// remain stable; every resource read/write must use this ownership ledger.
func (s *Service) Transfer(ctx context.Context, kind, id string, tenant uint64, targetSpace string) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	if _, e = s.resourceName(ctx, kind, id, tenant); e != nil {
		return e
	}
	a, e := s.Check(ctx, kind, id, tenant)
	if e != nil || !a.Manage {
		return ErrForbidden
	}
	if _, e = s.member(ctx, s.db, targetSpace, u.ID, 3); e != nil {
		return e
	}
	if e = s.requireWritableSpace(ctx, s.db, targetSpace); e != nil {
		return e
	}
	if kind == "agent" {
		if e = s.validateAgentPublication(ctx, targetSpace, id, tenant); e != nil {
			return e
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockResource(ctx, tx, kind, id, tenant); err != nil {
			return err
		}
		if err := New(tx).Require(ctx, kind, id, tenant, "manage"); err != nil {
			return err
		}
		if _, e := s.member(ctx, tx, targetSpace, u.ID, 3); e != nil {
			return e
		}
		if e := New(tx).requireWritableSpace(ctx, tx, targetSpace); e != nil {
			return e
		}
		if kind == "agent" {
			if e := New(tx).validateAgentPublication(ctx, targetSpace, id, tenant); e != nil {
				return e
			}
		}
		o := types.SpaceResource{Kind: kind, ResourceID: id, SourceTenantID: tenant, OwnerSpaceID: targetSpace, CreatedBy: u.ID, CreatedAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "kind"}, {Name: "resource_id"}, {Name: "source_tenant_id"}}, DoUpdates: clause.Assignments(map[string]any{"owner_user_id": "", "owner_space_id": targetSpace, "created_at": o.CreatedAt})}).Create(&o).Error; err != nil {
			return err
		}
		return s.event(ctx, tx, targetSpace, u.ID, "resource.transferred", kind+":"+id)
	})
}

func (s *Service) Events(ctx context.Context, space string) ([]types.SpaceEvent, error) {
	u, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if _, e = s.member(ctx, s.db, space, u.ID, 1); e != nil {
		return nil, e
	}
	rows := []types.SpaceEvent{}
	e = s.db.WithContext(ctx).Where("space_id=?", space).Order("id DESC").Limit(100).Find(&rows).Error
	return rows, e
}

func (s *Service) RegisterCreated(ctx context.Context, kind, id string, tenant uint64) error {
	target := types.CreatedResourceSpace(ctx)
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	if u.TenantID != tenant {
		return ErrForbidden
	}
	if kind == "agent" {
		var agent types.CustomAgent
		if e = s.db.WithContext(ctx).First(&agent, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
			return e
		}
		if e = s.ValidateAgentKnowledgeSelection(ctx, agent.Config); e != nil {
			return e
		}
	}
	if target == "" {
		return s.db.WithContext(ctx).Create(&types.SpaceResource{Kind: kind, ResourceID: id, SourceTenantID: tenant, OwnerUserID: u.ID, CreatedBy: u.ID, CreatedAt: time.Now().UTC()}).Error
	}
	if e = s.requireWritableSpace(ctx, s.db, target); e != nil {
		return e
	}

	if _, e = s.member(ctx, s.db, target, u.ID, 2); e != nil {
		return e
	}
	if kind == "agent" {
		if e = s.validateAgentPublication(ctx, target, id, tenant); e != nil {
			return e
		}
	}
	if e = s.db.WithContext(ctx).Create(&types.SpaceResource{Kind: kind, ResourceID: id, SourceTenantID: tenant, OwnerSpaceID: target, CreatedBy: u.ID, CreatedAt: time.Now().UTC()}).Error; e != nil {
		return e
	}
	return s.event(ctx, s.db, target, u.ID, "resource.created", kind+":"+id)
}

func (s *Service) InspectResource(ctx context.Context, kind, id string, tenant uint64) (map[string]any, error) {
	a, e := s.Check(ctx, kind, id, tenant)
	if e != nil || !a.Read {
		return nil, ErrForbidden
	}
	grants := []types.SpaceGrant{}
	if a.Manage {
		if e = s.db.WithContext(ctx).Where("kind=? AND resource_id=? AND source_tenant_id=?", kind, id, tenant).Find(&grants).Error; e != nil {
			return nil, e
		}
	}
	return map[string]any{"access": a, "source_tenant_id": tenant, "grants": grants}, nil
}
