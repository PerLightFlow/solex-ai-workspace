package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type SystemBuiltinAgentRepository interface {
	SeedSystemBuiltinAgents(context.Context, []*types.SystemBuiltinAgent) error
	ListSystemBuiltinAgents(context.Context) ([]*types.SystemBuiltinAgent, error)
	GetSystemBuiltinAgent(context.Context, string) (*types.SystemBuiltinAgent, error)
	CreateSystemBuiltinAgent(context.Context, *types.SystemBuiltinAgent) error
	UpdateSystemBuiltinAgent(context.Context, *types.SystemBuiltinAgent) error
	DeleteSystemBuiltinAgent(context.Context, string) error
}

// Kept separate from personal-agent CRUD so ownership never grants platform writes.
type SystemBuiltinAgentService interface {
	ListSystemBuiltinAgents(context.Context) ([]*types.CustomAgent, error)
	GetSystemBuiltinAgent(context.Context, string) (*types.CustomAgent, error)
	CreateSystemBuiltinAgent(context.Context, *types.CustomAgent) (*types.CustomAgent, error)
	UpdateSystemBuiltinAgent(context.Context, *types.CustomAgent, *string) (*types.CustomAgent, error)
	DeleteSystemBuiltinAgent(context.Context, string) error
}
