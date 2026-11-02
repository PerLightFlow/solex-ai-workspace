package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *customAgentRepository) SeedSystemBuiltinAgents(ctx context.Context, agents []*types.SystemBuiltinAgent) error {
	if len(agents) == 0 {
		return nil
	}
	// Conflict includes soft-deleted rows. Never Unscoped().Delete or upsert here.
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&agents).Error
}

func (r *customAgentRepository) ListSystemBuiltinAgents(ctx context.Context) ([]*types.SystemBuiltinAgent, error) {
	var agents []*types.SystemBuiltinAgent
	err := r.db.WithContext(ctx).Order("sort_order ASC, created_at ASC, id ASC").Find(&agents).Error
	return agents, err
}

func (r *customAgentRepository) GetSystemBuiltinAgent(ctx context.Context, id string) (*types.SystemBuiltinAgent, error) {
	var agent types.SystemBuiltinAgent
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&agent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCustomAgentNotFound
	}
	return &agent, err
}

func (r *customAgentRepository) CreateSystemBuiltinAgent(ctx context.Context, agent *types.SystemBuiltinAgent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *customAgentRepository) UpdateSystemBuiltinAgent(ctx context.Context, agent *types.SystemBuiltinAgent) error {
	// Updates instead of Save: a concurrent delete must not resurrect the row.
	result := r.db.WithContext(ctx).Model(&types.SystemBuiltinAgent{}).Where("id = ?", agent.ID).
		Select("name", "description", "avatar", "config", "customized", "updated_at").Updates(agent)
	if result.Error == nil && result.RowsAffected == 0 {
		return ErrCustomAgentNotFound
	}
	return result.Error
}

func (r *customAgentRepository) DeleteSystemBuiltinAgent(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.SystemBuiltinAgent{})
	if result.Error == nil && result.RowsAffected == 0 {
		return ErrCustomAgentNotFound
	}
	return result.Error
}
