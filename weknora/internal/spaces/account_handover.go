package spaces

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// AccountHandover is an administrator's preflight for disabling one account.
// Only collaboration metadata is included, never unshared content or chats.
type AccountHandover struct {
	UserID    string           `json:"user_id"`
	Username  string           `json:"username"`
	IsActive  bool             `json:"is_active"`
	Spaces    []HandoverSpace  `json:"spaces"`
	Resources []SharedHandover `json:"resources"`
}

func (s *Service) UserHandover(ctx context.Context, id string) (*AccountHandover, error) {
	u, err := s.actor(ctx)
	if err != nil || !u.IsSystemAdmin {
		return nil, ErrForbidden
	}
	var target types.User
	if err := s.db.WithContext(ctx).First(&target, "id=?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	result := &AccountHandover{UserID: target.ID, Username: target.Username, IsActive: target.IsActive, Spaces: []HandoverSpace{}}
	if err := s.handoverSpacesQuery(ctx, id, false).Scan(&result.Spaces).Error; err != nil {
		return nil, err
	}
	result.Resources, err = s.sharedHandovers(ctx, id, false)
	return result, err
}

func (s *Service) handoverSpacesQuery(ctx context.Context, owner string, orphanOnly bool) *gorm.DB {
	q := s.db.WithContext(ctx).Table("organizations o").
		Select("o.id,o.name,o.owner_id,u.username AS owner_name").
		Joins("JOIN users u ON u.id=o.owner_id").Where("o.deleted_at IS NULL")
	if owner != "" {
		q = q.Where("o.owner_id=?", owner)
	}
	if orphanOnly {
		q = q.Where("NOT u.is_active OR u.deleted_at IS NOT NULL")
	}
	return q.Order("o.name,o.id")
}

func (s *Service) sharedHandovers(ctx context.Context, owner string, orphanOnly bool) ([]SharedHandover, error) {
	q := s.db.WithContext(ctx).Table("solex_space_resources r").
		Select("r.kind,r.resource_id,r.source_tenant_id,r.owner_user_id,g.space_id,o.name AS space_name,u.username AS owner_name").
		Joins("JOIN users u ON u.id=r.owner_user_id").
		Joins("JOIN solex_space_grants g ON g.kind=r.kind AND g.resource_id=r.resource_id AND g.source_tenant_id=r.source_tenant_id").
		Joins("JOIN organizations o ON o.id=g.space_id AND o.deleted_at IS NULL")
	if owner != "" {
		q = q.Where("r.owner_user_id=?", owner)
	}
	if orphanOnly {
		q = q.Where("NOT u.is_active OR u.deleted_at IS NOT NULL")
	}
	rows := []SharedHandover{}
	if err := q.Order("r.kind,r.resource_id,r.source_tenant_id,g.space_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := []SharedHandover{}
	for _, row := range rows {
		name, err := s.resourceName(ctx, row.Kind, row.ResourceID, row.SourceTenantID)
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		row.Name = name
		result = append(result, row)
	}
	return result, nil
}
