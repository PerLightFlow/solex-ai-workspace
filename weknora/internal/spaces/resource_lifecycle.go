package spaces

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// RecycleResource hides the entire resource immediately. Its IDs, original
// files and indexes stay intact until the recovery window expires.
func (s *Service) RecycleResource(ctx context.Context, kind, id string, tenant uint64) error {
	if kind != "knowledge_base" && kind != "agent" {
		return ErrInvalid
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p := New(tx)
		if e := lockResource(ctx, tx, kind, id, tenant); e != nil {
			return e
		}
		if a, e := p.checkAccess(ctx, kind, id, tenant); e != nil || !a.Manage {
			return ErrForbidden
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		row := RecycledContent{Kind: kind, ResourceID: id, TenantID: tenant, ActorID: types.CallerFromContext(ctx).UserID, DeletedAt: now, ExpiresAt: now.Add(time.Duration(p.Retention(ctx).RecycleDays) * 24 * time.Hour)}
		if kind == "knowledge_base" {
			var kb types.KnowledgeBase
			if e := tx.First(&kb, "id=?", id).Error; e != nil {
				return e
			}
			data, e := json.Marshal(&kb)
			if e != nil {
				return e
			}
			row.Payload = string(data)
			row.Title = kb.Name
			row.KBID = id
			var files []*types.Knowledge
			if e := tx.Where("knowledge_base_id=?", id).Find(&files).Error; e != nil {
				return e
			}
			for _, file := range files {
				if e := p.retainFile(ctx, file); e != nil {
					return e
				}
				if _, e := p.recyclePayload(ctx, file); e != nil {
					return e
				}
			}
			for _, model := range []any{&types.Knowledge{}, &types.WikiPage{}, &types.Chunk{}} {
				if e := tx.Model(model).Where("knowledge_base_id=? AND deleted_at IS NULL", id).Update("deleted_at", now).Error; e != nil {
					return e
				}
			}
			if e := tx.Model(&kb).Update("deleted_at", now).Error; e != nil {
				return e
			}
		} else {
			var agent types.CustomAgent
			if e := tx.First(&agent, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
				return e
			}
			data, e := json.Marshal(&agent)
			if e != nil {
				return e
			}
			row.Payload = string(data)
			row.Title = agent.Name
			if e := tx.Model(&agent).Update("deleted_at", now).Error; e != nil {
				return e
			}
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
	})
}

func (s *Service) RecycledResources(ctx context.Context) ([]RecycledContent, error) {
	if _, e := s.actor(ctx); e != nil {
		return nil, e
	}
	rows := []RecycledContent{}
	if e := s.db.WithContext(ctx).Where("kind IN ? AND expires_at>?", []string{"knowledge_base", "agent"}, time.Now()).Order("deleted_at DESC").Find(&rows).Error; e != nil {
		return nil, e
	}
	out := []RecycledContent{}
	for _, r := range rows {
		a, e := s.checkAccess(ctx, r.Kind, r.ResourceID, r.TenantID)
		if e != nil {
			return nil, e
		}
		if a.Manage {
			r.Payload = ""
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Service) RestoreResource(ctx context.Context, kind, id string, tenant uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p := New(tx)
		if a, e := p.checkAccess(ctx, kind, id, tenant); e != nil || !a.Manage {
			return ErrForbidden
		}
		var r RecycledContent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "kind=? AND resource_id=? AND tenant_id=? AND expires_at>?", kind, id, tenant, time.Now()).Error; e != nil {
			return ErrNotFound
		}
		if kind == "knowledge_base" {
			for _, model := range []any{&types.Knowledge{}, &types.WikiPage{}, &types.Chunk{}} {
				if e := tx.Unscoped().Model(model).Where("knowledge_base_id=? AND deleted_at=?", id, r.DeletedAt).Update("deleted_at", nil).Error; e != nil {
					return e
				}
			}
			if e := tx.Unscoped().Model(&types.KnowledgeBase{}).Where("id=? AND tenant_id=?", id, tenant).Update("deleted_at", nil).Error; e != nil {
				return e
			}
		} else if kind == "agent" {
			// Publication is checked again: a missing KB grant must not be revived.
			var agent types.CustomAgent
			if e := json.Unmarshal([]byte(r.Payload), &agent); e != nil {
				return e
			}
			if e := p.ValidateAgentUpdate(ctx, &agent, agent.Config); e != nil {
				return e
			}
			if e := tx.Unscoped().Model(&types.CustomAgent{}).Where("id=? AND tenant_id=?", id, tenant).Update("deleted_at", nil).Error; e != nil {
				return e
			}
		} else {
			return ErrInvalid
		}
		return tx.Delete(&r).Error
	})
}
