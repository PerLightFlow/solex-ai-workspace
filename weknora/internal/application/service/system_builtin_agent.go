package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

func (s *customAgentService) systemBuiltinCatalog() interfaces.SystemBuiltinAgentRepository {
	if !types.ManagedRuntimeEnabled() {
		return nil
	}
	r, _ := s.repo.(interfaces.SystemBuiltinAgentRepository)
	return r
}

func (s *customAgentService) seedSystemBuiltinAgents(ctx context.Context, r interfaces.SystemBuiltinAgentRepository) error {
	s.builtinSeedMu.Lock()
	defer s.builtinSeedMu.Unlock()
	if s.builtinSeeded {
		return nil
	}
	order := map[string]int{}
	for i, id := range types.GetBuiltinAgentIDs() {
		order[id] = i
	}
	seeds := []*types.SystemBuiltinAgent{}
	for _, id := range types.AllBuiltinAgentIDs() {
		a := types.GetBuiltinAgent(id, 0)
		if a == nil {
			continue
		}
		n, visible := order[id]
		if !visible {
			n = len(order)
		}
		// Seed the shipped template, never another tenant's private override.
		clearSystemBuiltinBindings(&a.Config)
		seeds = append(seeds, &types.SystemBuiltinAgent{ID: id, Name: a.Name,
			Description: a.Description, Avatar: a.Avatar, Config: a.Config,
			IsInternal: !visible, SortOrder: n})
	}
	if err := r.SeedSystemBuiltinAgents(ctx, seeds); err != nil {
		return err
	}
	s.builtinSeeded = true
	return nil
}

func (s *customAgentService) resolveSystemBuiltinAgent(ctx context.Context, id string) (*types.CustomAgent, error) {
	r := s.systemBuiltinCatalog()
	if r == nil {
		return nil, ErrAgentNotFound
	}
	if err := s.seedSystemBuiltinAgents(ctx, r); err != nil {
		return nil, err
	}
	a, err := r.GetSystemBuiltinAgent(ctx, id)
	if errors.Is(err, repository.ErrCustomAgentNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, ErrInvalidTenantID
	}
	result := a.ForTenant(tenantID)
	if !a.Customized {
		types.ApplyBuiltinAgentLocalization(ctx, result)
	}
	result.EnsureDefaults()
	normalizeManagedAgentModels(ctx, &result.Config)
	return result, nil
}

func (s *customAgentService) listSystemBuiltinAgents(ctx context.Context, includeInternal bool) ([]*types.CustomAgent, error) {
	r := s.systemBuiltinCatalog()
	if r == nil {
		return nil, ErrAgentNotFound
	}
	if err := s.seedSystemBuiltinAgents(ctx, r); err != nil {
		return nil, err
	}
	rows, err := r.ListSystemBuiltinAgents(ctx)
	if err != nil {
		return nil, err
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		return nil, ErrInvalidTenantID
	}
	result := make([]*types.CustomAgent, 0, len(rows))
	for _, row := range rows {
		if row.IsInternal && !includeInternal {
			continue
		}
		a := row.ForTenant(tenantID)
		if !row.Customized {
			types.ApplyBuiltinAgentLocalization(ctx, a)
		}
		a.EnsureDefaults()
		normalizeManagedAgentModels(ctx, &a.Config)
		result = append(result, a)
	}
	return result, nil
}

func (s *customAgentService) requireSystemBuiltinAdmin(ctx context.Context) (interfaces.SystemBuiltinAgentRepository, error) {
	// A tenant Owner, synthetic API-key identity or role alone never qualifies.
	_, apiKey := types.TenantAPIKeyScopeFromContext(ctx)
	if !types.IsSystemAdminFromContext(ctx) || apiKey {
		return nil, apperrors.NewForbiddenError("只有系统管理员可以管理全局内置智能体")
	}
	r := s.systemBuiltinCatalog()
	if r == nil {
		return nil, apperrors.NewBadRequestError("系统内置智能体管理未启用")
	}
	if err := s.seedSystemBuiltinAgents(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *customAgentService) ListSystemBuiltinAgents(ctx context.Context) ([]*types.CustomAgent, error) {
	if _, err := s.requireSystemBuiltinAdmin(ctx); err != nil {
		return nil, err
	}
	return s.listSystemBuiltinAgents(ctx, true)
}

func (s *customAgentService) GetSystemBuiltinAgent(ctx context.Context, id string) (*types.CustomAgent, error) {
	if _, err := s.requireSystemBuiltinAdmin(ctx); err != nil {
		return nil, err
	}
	return s.resolveSystemBuiltinAgent(ctx, id)
}

// Platform templates may select all/none of the CALLER'S resources. A concrete
// resource ID would bind an administrator's workspace to every other user.
func validateSystemBuiltinConfig(cfg types.CustomAgentConfig) error {
	if (cfg.KBSelectionMode != "" && cfg.KBSelectionMode != "all" && cfg.KBSelectionMode != "none") || len(cfg.KnowledgeBases) != 0 {
		return apperrors.NewBadRequestError("全局智能体知识范围只能为当前用户可用知识库或不使用知识库，不能绑定私人知识库")
	}
	if (cfg.MCPSelectionMode != "" && cfg.MCPSelectionMode != "all" && cfg.MCPSelectionMode != "none") || len(cfg.MCPServices) != 0 ||
		(cfg.SkillsSelectionMode != "" && cfg.SkillsSelectionMode != "all" && cfg.SkillsSelectionMode != "none") || len(cfg.SelectedSkills) != 0 {
		return apperrors.NewBadRequestError("全局智能体不能绑定某个工作空间的 MCP 或技能")
	}
	if cfg.ModelID != "" || cfg.RerankModelID != "" || cfg.VLMModelID != "" || cfg.ASRModelID != "" || cfg.QueryUnderstandModelID != "" ||
		(cfg.QuestionSuggestions != nil && cfg.QuestionSuggestions.FollowUps.ModelID != "") ||
		cfg.SandboxConfigID != "" || cfg.WebSearchProviderID != "" || cfg.ImageStorageProvider != "" || len(cfg.ChatParserEngineRules) != 0 {
		return apperrors.NewBadRequestError("全局智能体使用系统默认模型和运行环境，不能绑定私人模型、解析器或连接配置")
	}
	return nil
}

func clearSystemBuiltinBindings(cfg *types.CustomAgentConfig) {
	cfg.ModelID, cfg.RerankModelID, cfg.VLMModelID, cfg.ASRModelID, cfg.QueryUnderstandModelID = "", "", "", "", ""
	cfg.SandboxConfigID, cfg.WebSearchProviderID, cfg.ImageStorageProvider = "", "", ""
	cfg.ChatParserEngineRules, cfg.KnowledgeBases, cfg.MCPServices, cfg.SelectedSkills = nil, nil, nil, nil
	if cfg.KBSelectionMode == "selected" {
		cfg.KBSelectionMode = "none"
	}
	if cfg.MCPSelectionMode == "selected" {
		cfg.MCPSelectionMode = "none"
	}
	if cfg.SkillsSelectionMode == "selected" {
		cfg.SkillsSelectionMode = "none"
	}
	if cfg.QuestionSuggestions != nil {
		copy := *cfg.QuestionSuggestions
		copy.FollowUps.ModelID = ""
		cfg.QuestionSuggestions = &copy
	}
}

func validateSystemBuiltinAgent(a *types.CustomAgent) error {
	if strings.TrimSpace(a.Name) == "" {
		return ErrAgentNameRequired
	}
	if len([]rune(a.Name)) > 255 {
		return apperrors.NewBadRequestError("智能体名称不能超过255个字符")
	}
	if err := a.ValidateAvatar(); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	if a.Config.AgentMode != types.AgentModeQuickAnswer && a.Config.AgentMode != types.AgentModeSmartReasoning {
		return apperrors.NewBadRequestError("无效的智能体运行模式")
	}
	if err := a.Config.QuestionSuggestions.Validate(); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	return validateSystemBuiltinConfig(a.Config)
}

func (s *customAgentService) CreateSystemBuiltinAgent(ctx context.Context, a *types.CustomAgent) (*types.CustomAgent, error) {
	r, err := s.requireSystemBuiltinAdmin(ctx)
	if err != nil {
		return nil, err
	}
	a.EnsureCreationDefaults()
	if err := validateSystemBuiltinAgent(a); err != nil {
		return nil, err
	}
	// Ignore caller IDs and flags: only shipped definitions receive privileged IDs.
	id := types.SystemBuiltinAgentPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	row := &types.SystemBuiltinAgent{ID: id, Name: strings.TrimSpace(a.Name), Description: a.Description,
		Avatar: a.Avatar, Config: a.Config, SortOrder: 1000, Customized: true}
	if err := r.CreateSystemBuiltinAgent(ctx, row); err != nil {
		return nil, err
	}
	return s.resolveSystemBuiltinAgent(ctx, id)
}

func (s *customAgentService) UpdateSystemBuiltinAgent(ctx context.Context, a *types.CustomAgent, avatar *string) (*types.CustomAgent, error) {
	r, err := s.requireSystemBuiltinAdmin(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := r.GetSystemBuiltinAgent(ctx, a.ID)
	if errors.Is(err, repository.ErrCustomAgentNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, err
	}
	if avatar == nil {
		a.Avatar = existing.Avatar
	} else {
		a.Avatar = *avatar
	}
	a.EnsureDefaults()
	if (a.ID == types.BuiltinQuickAnswerID || a.ID == types.BuiltinSmartReasoningID || existing.IsInternal) && a.Config.AgentMode != existing.Config.AgentMode {
		return nil, apperrors.NewBadRequestError("此内置流程的运行模式不可更改")
	}
	if err := validateSystemBuiltinAgent(a); err != nil {
		return nil, err
	}
	existing.Name, existing.Description, existing.Avatar = strings.TrimSpace(a.Name), a.Description, a.Avatar
	existing.Config, existing.Customized, existing.UpdatedAt = a.Config, true, time.Now()
	if err := r.UpdateSystemBuiltinAgent(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrCustomAgentNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	return s.resolveSystemBuiltinAgent(ctx, a.ID)
}

func (s *customAgentService) DeleteSystemBuiltinAgent(ctx context.Context, id string) error {
	r, err := s.requireSystemBuiltinAdmin(ctx)
	if err != nil {
		return err
	}
	err = r.DeleteSystemBuiltinAgent(ctx, id)
	if errors.Is(err, repository.ErrCustomAgentNotFound) {
		return ErrAgentNotFound
	}
	return err
}
