package spaces

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

// Catalog is shared by resource pickers and the space page, so hiding a space
// does not leave a second tenant-based resource enumeration path behind.
func (s *Service) Catalog(ctx context.Context, kind string) ([]Resource, error) {
	list, e := s.List(ctx)
	if e != nil {
		return nil, e
	}
	result := []Resource{}
	seen := map[string]bool{}
	for _, space := range list {
		rows, err := s.Resources(ctx, space.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.Kind != kind {
				continue
			}
			key := fmt.Sprintf("%s:%d", r.ID, r.SourceTenantID)
			if !seen[key] {
				result = append(result, r)
				seen[key] = true
			}
		}
	}
	return result, nil
}

func (s *Service) SharedKnowledgeBases(ctx context.Context) ([]*types.SharedKnowledgeBaseInfo, error) {
	rows, e := s.Catalog(ctx, "knowledge_base")
	if e != nil {
		return nil, e
	}
	result := []*types.SharedKnowledgeBaseInfo{}
	for _, r := range rows {
		var kb types.KnowledgeBase
		if e = s.db.WithContext(ctx).First(&kb, "id=?", r.ID).Error; e != nil {
			continue
		}
		role := types.OrgRoleViewer
		if r.Access.Edit {
			role = types.OrgRoleEditor
		}
		if r.Access.Manage {
			role = types.OrgRoleAdmin
		}
		info := &types.SharedKnowledgeBaseInfo{KnowledgeBase: &kb, Permission: role, SourceTenantID: r.SourceTenantID}
		if len(r.Access.Sources) > 0 {
			src := r.Access.Sources[0]
			info.OrganizationID = src.SpaceID
			info.OrgName = src.SpaceName
			info.SharedAt = src.HistoryFrom
		}
		if r.Grant != nil {
			info.ShareID = r.Grant.ID
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *Service) SharedAgents(ctx context.Context) ([]*types.SharedAgentInfo, error) {
	rows, e := s.Catalog(ctx, "agent")
	if e != nil {
		return nil, e
	}
	result := []*types.SharedAgentInfo{}
	for _, r := range rows {
		a, err := s.Agent(ctx, r.ID, r.SourceTenantID)
		if err != nil {
			continue
		}
		role := types.OrgRoleViewer
		if r.Access.Edit {
			role = types.OrgRoleEditor
		}
		if r.Access.Manage {
			role = types.OrgRoleAdmin
		}
		info := &types.SharedAgentInfo{Agent: a, Permission: role, SourceTenantID: r.SourceTenantID}
		if len(r.Access.Sources) > 0 {
			src := r.Access.Sources[0]
			info.OrganizationID = src.SpaceID
			info.OrgName = src.SpaceName
			info.SharedAt = src.HistoryFrom
		}
		if r.Grant != nil {
			info.ShareID = r.Grant.ID
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *Service) FindAgent(ctx context.Context, id string) (*types.CustomAgent, error) {
	rows, e := s.Catalog(ctx, "agent")
	if e != nil {
		return nil, e
	}
	var match *Resource
	for i := range rows {
		if rows[i].ID == id {
			if match != nil {
				return nil, ErrInvalid
			}
			match = &rows[i]
		}
	}
	if match == nil {
		return nil, ErrNotFound
	}
	return s.Agent(ctx, id, match.SourceTenantID)
}
