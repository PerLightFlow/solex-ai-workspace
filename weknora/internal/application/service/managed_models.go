package service

import (
	"context"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Tenant-only worker contexts use the same shared catalog as human members.
// Administrative access must be explicit; workspace ownership is not enough.
func managedModelRestricted(ctx context.Context) bool {
	return types.ManagedRuntimeEnabled() && !types.IsSystemAdminFromContext(ctx)
}

func requireManagedModelAdministrator(ctx context.Context) error {
	if managedModelRestricted(ctx) {
		return apperrors.NewForbiddenError("模型由系统管理员统一配置")
	}
	return nil
}

// managedDefaultModel does not silently pick an arbitrary usable model. A
// purpose default from the central workspace or an explicit built-in default
// is required, so an incomplete deployment produces an actionable error.
func managedDefaultModel(ctx context.Context, svc interfaces.ModelService, kind types.ModelType) (*types.Model, error) {
	if svc == nil {
		return nil, apperrors.NewBadRequestError("系统模型尚未配置，请联系管理员")
	}
	models, err := svc.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	if model := types.PickManagedDefaultModel(models, kind); model != nil {
		return model, nil
	}
	return nil, apperrors.NewBadRequestError("系统尚未配置可用的 " + string(kind) + " 默认模型，请联系管理员")
}

// Empty means follow the central default at execution time, not an unfinished
// agent. Business prompts and knowledge selections remain user-editable.
func normalizeManagedAgentModels(ctx context.Context, cfg *types.CustomAgentConfig) {
	if !managedModelRestricted(ctx) {
		return
	}
	cfg.ModelID = ""
	cfg.RerankModelID = ""
	cfg.QueryUnderstandModelID = ""
	cfg.VLMModelID = ""
	cfg.Temperature = 0
	cfg.MaxCompletionTokens = 0
	cfg.Thinking = nil
	cfg.ReasoningEffort = ""
	cfg.LLMCallTimeout = 0
	cfg.ASRModelID = ""
	// The effective budget comes from platform settings when a run starts.
	// Zero here removes a user-supplied override without baking in a budget.
	cfg.MaxIterations = 0
	cfg.SandboxConfigID = ""
	cfg.ChatParserEngineRules = nil
	cfg.ImageStorageProvider = ""
	cfg.WebSearchProviderID = ""
	cfg.MCPAuthWaitTimeout = 0
	cfg.AccessLevel = types.AssistantAccessConfirm
	if cfg.QuestionSuggestions != nil {
		copy := *cfg.QuestionSuggestions
		copy.FollowUps.ModelID = ""
		cfg.QuestionSuggestions = &copy
	}
}

func managedModelBinding(ctx context.Context, svc interfaces.ModelService, requested string, kind types.ModelType) (string, error) {
	model, err := managedDefaultModel(ctx, svc, kind)
	if err != nil {
		return "", err
	}
	if requested = strings.TrimSpace(requested); requested != "" && requested != model.ID {
		return "", apperrors.NewBadRequestError("模型配置由系统管理员统一管理，请使用系统默认模型")
	}
	return model.ID, nil
}

// Resolve optional execution models from the same catalog. The primary model
// is validated separately; optional capabilities remain off when unconfigured.
func (s *sessionService) applyManagedAgentModelDefaults(ctx context.Context, cfg *types.CustomAgentConfig) {
	normalizeManagedAgentModels(ctx, cfg)
	if s.cfg != nil && s.cfg.Conversation != nil && s.cfg.Conversation.Summary != nil {
		defaults := s.cfg.Conversation.Summary
		cfg.Temperature = defaults.Temperature
		cfg.MaxCompletionTokens = defaults.MaxCompletionTokens
		cfg.Thinking = defaults.Thinking
	}
	if s.cfg != nil && s.cfg.Agent != nil {
		cfg.LLMCallTimeout = s.cfg.Agent.LLMCallTimeout
	}
	if s.modelService == nil {
		return
	}
	models, err := s.modelService.ListModels(ctx)
	if err != nil {
		return
	}
	for kind, target := range map[types.ModelType]*string{
		types.ModelTypeRerank: &cfg.RerankModelID,
		types.ModelTypeVLLM:   &cfg.VLMModelID,
		types.ModelTypeASR:    &cfg.ASRModelID,
	} {
		if model := types.PickManagedDefaultModel(models, kind); model != nil {
			*target = model.ID
		}
	}
}

// Apply before leases, attachment routing and optional rerank checks, not only
// when constructing the final LLM request. Workers use this path as well.
func (s *sessionService) withManagedAgentModelDefaults(ctx context.Context, req *types.QARequest) *types.QARequest {
	if req == nil || req.CustomAgent == nil {
		return req
	}
	globalBuiltin := types.ManagedRuntimeEnabled() && req.CustomAgent.IsBuiltin && types.IsSystemBuiltinAgentID(req.CustomAgent.ID)
	if !managedModelRestricted(ctx) && !globalBuiltin {
		return req
	}
	copy := *req
	agent := *req.CustomAgent
	s.applyManagedAgentModelDefaults(ctx, &agent.Config)
	copy.CustomAgent = &agent
	return &copy
}
