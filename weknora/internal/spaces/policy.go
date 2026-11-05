package spaces

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) IsPersonalOwner(ctx context.Context, kind, id string, tenant uint64) bool {
	u, e := s.actor(ctx)
	if e != nil {
		return false
	}
	var o types.SpaceResource
	e = s.db.WithContext(ctx).First(&o, "kind=? AND resource_id=? AND source_tenant_id=?", kind, id, tenant).Error
	return (errors.Is(e, gorm.ErrRecordNotFound) && u.TenantID == tenant) || (e == nil && o.OwnerUserID == u.ID)
}

func (s *Service) Require(ctx context.Context, kind, id string, tenant uint64, permission string) error {
	a, e := s.Check(ctx, kind, id, tenant)
	if e != nil {
		return e
	}
	allowed := a.Read
	switch permission {
	case "edit":
		allowed = a.Edit
	case "manage":
		allowed = a.Manage
	case "download":
		allowed = a.Download
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// ValidateAgentUpdate checks every publication, including space ownership.
// Editing the original cannot silently widen a receiving space's authority.
func (s *Service) ValidateAgentUpdate(ctx context.Context, agent *types.CustomAgent, config types.CustomAgentConfig) error {
	if e := s.ValidateAgentKnowledgeSelection(ctx, config); e != nil {
		return e
	}
	var grants []types.SpaceGrant
	if e := s.db.WithContext(ctx).Where("kind='agent' AND resource_id=? AND source_tenant_id=?", agent.ID, agent.TenantID).Find(&grants).Error; e != nil {
		return e
	}
	var owners []types.SpaceResource
	if e := s.db.WithContext(ctx).Where("kind='agent' AND resource_id=? AND source_tenant_id=? AND owner_space_id<>''", agent.ID, agent.TenantID).Find(&owners).Error; e != nil {
		return e
	}
	for _, g := range grants {
		if e := s.validateAgentConfig(ctx, g.SpaceID, config); e != nil {
			return e
		}
	}
	for _, o := range owners {
		if e := s.validateAgentConfig(ctx, o.OwnerSpaceID, config); e != nil {
			return e
		}
	}
	return nil
}

// Saving explicit IDs cannot attach another account's private knowledge.
// Publication adds the receiving space's independent dependency check.
func (s *Service) ValidateAgentKnowledgeSelection(ctx context.Context, config types.CustomAgentConfig) error {
	if config.KBSelectionMode != "selected" {
		return nil
	}
	for _, id := range config.KnowledgeBases {
		kind, resource, tenant, e := s.ResourceScope(ctx, "knowledge_base", id, 0)
		if e != nil {
			return ErrForbidden
		}
		if e = s.Require(ctx, kind, resource, tenant, "read"); e != nil {
			return ErrForbidden
		}
	}
	return nil
}

// ResourceScope resolves IDs before authorizing; knowledge and chunk IDs never
// confer rights through their original storage tenant.
func (s *Service) ResourceScope(ctx context.Context, kind, id string, tenant uint64) (string, string, uint64, error) {
	switch kind {
	case "knowledge_base":
		var k types.KnowledgeBase
		if e := s.db.WithContext(ctx).First(&k, "id=?", id).Error; e != nil {
			return "", "", 0, ErrNotFound
		}
		return "knowledge_base", k.ID, k.TenantID, nil
	case "knowledge":
		var k types.Knowledge
		if e := s.db.WithContext(ctx).First(&k, "id=?", id).Error; e != nil {
			return "", "", 0, ErrNotFound
		}
		return "knowledge_base", k.KnowledgeBaseID, k.TenantID, nil
	case "chunk":
		var k types.Chunk
		if e := s.db.WithContext(ctx).First(&k, "id=?", id).Error; e != nil {
			return "", "", 0, ErrNotFound
		}
		return "knowledge_base", k.KnowledgeBaseID, k.TenantID, nil
	case "agent":
		var a types.CustomAgent
		if e := s.db.WithContext(ctx).First(&a, "id=? AND tenant_id=?", id, tenant).Error; e != nil {
			return "", "", 0, ErrNotFound
		}
		return kind, id, tenant, nil
	}
	return "", "", 0, ErrInvalid
}

func lockResource(ctx context.Context, tx *gorm.DB, kind, id string, tenant uint64) error {
	var out struct{ ID string }
	table := "knowledge_bases"
	if kind == "agent" {
		table = "custom_agents"
	} else if kind != "knowledge_base" {
		return ErrInvalid
	}
	return tx.WithContext(ctx).Table(table).Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND deleted_at IS NULL", id, tenant).Take(&out).Error
}
