package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func managedMemberContext(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "10000")
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(22))
	return context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleOwner)
}

func managedTestModel(id string, kind types.ModelType) *types.Model {
	return &types.Model{ID: id, Type: kind, IsBuiltin: true, IsDefault: true, Status: types.ModelStatusActive}
}

func TestManagedModelCatalogUsesCentralDefaultsForMembersAndWorkers(t *testing.T) {
	ctx := managedMemberContext(t)
	repo := &defaultsModelRepository{models: []*types.Model{
		managedTestModel("shared-a", types.ModelTypeKnowledgeQA),
		managedTestModel("shared-b", types.ModelTypeKnowledgeQA),
		{ID: "private", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsDefault: true},
	}}
	tenants := &defaultsTenantService{tenant: &types.Tenant{ID: 10000, DefaultSettings: &types.DefaultSettings{ChatModelID: "shared-b"}}}
	svc := &modelService{repo: repo, tenantService: tenants}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 22, DefaultSettings: &types.DefaultSettings{ChatModelID: "private"}})
	models, err := svc.ListModels(ctx)
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, uint64(10000), tenants.requestedID)
	assert.Equal(t, "shared-b", types.PickDefaultModel(models, types.ModelTypeKnowledgeQA).ID)
	assert.Equal(t, uint64(22), types.MustTenantIDFromContext(ctx), "data ownership must not switch to the config workspace")
	// Worker contexts do not have a user or loaded workspace info.
	worker := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(22))
	models, err = svc.ListModels(worker)
	require.NoError(t, err)
	assert.Equal(t, "shared-b", types.PickDefaultModel(models, types.ModelTypeKnowledgeQA).ID)
}

func TestManagedModelMembersCannotUsePrivateModelOrMutateCatalog(t *testing.T) {
	ctx := managedMemberContext(t)
	repo := &stubCopyModelRepo{source: &types.Model{ID: "private", Status: types.ModelStatusActive}}
	svc := &modelService{repo: repo}
	_, err := svc.GetModelByID(ctx, "private")
	require.ErrorIs(t, err, ErrModelNotFound)
	_, err = svc.GetChatModel(ctx, "private")
	require.ErrorIs(t, err, ErrModelNotFound)
	_, err = svc.GetVLMModel(ctx, "private")
	require.ErrorIs(t, err, ErrModelNotFound)
	_, err = svc.GetASRModel(ctx, "private")
	require.ErrorIs(t, err, ErrModelNotFound)
	_, err = svc.GetEmbeddingModelForTenant(ctx, "private", 22)
	require.ErrorIs(t, err, ErrModelNotFound)
	require.Error(t, svc.CreateModel(ctx, &types.Model{Source: types.ModelSourceRemote}))
	require.Error(t, svc.UpdateModel(ctx, repo.source))
	require.Error(t, svc.DeleteModel(ctx, "private"))
	_, err = svc.CopyModel(ctx, "private", "copy")
	require.Error(t, err)
	assert.Nil(t, repo.created)
	assert.Zero(t, repo.updates)
	admin := context.WithValue(ctx, types.SystemAdminContextKey, true)
	_, err = svc.GetModelByID(admin, "private")
	require.NoError(t, err)
	require.NoError(t, svc.CreateModel(admin, &types.Model{Source: types.ModelSourceRemote, IsBuiltin: false}))
	require.NotNil(t, repo.created)
	assert.True(t, repo.created.IsBuiltin, "server must publish models even when client omits builtin")
}

func TestManagedAgentWithoutModelRunsAndFollowsChangedDefault(t *testing.T) {
	ctx := managedMemberContext(t)
	agents := &customAgentService{repo: &captureAgentRepo{}}
	created, err := agents.CreateAgent(ctx, &types.CustomAgent{Name: "Project helper", Config: types.CustomAgentConfig{SystemPrompt: "Explain sources", ModelID: "private", MaxIterations: -1, Temperature: 0.99}})
	require.NoError(t, err)
	assert.Empty(t, created.Config.ModelID)
	assert.Equal(t, "Explain sources", created.Config.SystemPrompt)
	assert.Equal(t, 10, created.Config.MaxIterations)
	models := &stubModelService{availableModels: []*types.Model{managedTestModel("default-a", types.ModelTypeKnowledgeQA)}}
	svc := &sessionService{modelService: models}
	req := &types.QARequest{CustomAgent: created, SummaryModelID: "private"}
	id, tenant, err := svc.resolveChatModelID(ctx, req, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "default-a", id)
	assert.Equal(t, uint64(22), tenant)
	models.availableModels = []*types.Model{managedTestModel("default-b", types.ModelTypeKnowledgeQA)}
	id, _, err = svc.resolveChatModelID(ctx, req, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "default-b", id)
	models.availableModels = []*types.Model{{ID: "unapproved", Type: types.ModelTypeKnowledgeQA, IsDefault: true, Status: types.ModelStatusActive}}
	_, _, err = svc.resolveChatModelID(ctx, req, nil, nil)
	require.ErrorContains(t, err, "管理员")
}

func TestSystemBuiltinAdministratorUsesCentralOptionalModels(t *testing.T) {
	ctx := context.WithValue(managedMemberContext(t), types.SystemAdminContextKey, true)
	svc := &sessionService{modelService: &stubModelService{availableModels: []*types.Model{
		managedTestModel("shared-rerank", types.ModelTypeRerank),
		managedTestModel("shared-vision", types.ModelTypeVLLM),
		managedTestModel("shared-audio", types.ModelTypeASR),
	}}}
	req := &types.QARequest{CustomAgent: &types.CustomAgent{ID: "builtin-global-test", IsBuiltin: true, TenantID: 22}}
	got := svc.withManagedAgentModelDefaults(ctx, req)
	require.Equal(t, "shared-rerank", got.CustomAgent.Config.RerankModelID)
	require.Equal(t, "shared-vision", got.CustomAgent.Config.VLMModelID)
	require.Equal(t, "shared-audio", got.CustomAgent.Config.ASRModelID)
	require.EqualValues(t, 22, got.CustomAgent.TenantID)
	require.Empty(t, req.CustomAgent.Config.VLMModelID, "runtime overlay must leave the catalog unchanged")
	personal := &types.QARequest{CustomAgent: &types.CustomAgent{ID: "personal", Config: types.CustomAgentConfig{VLMModelID: "admin-personal"}}}
	require.Same(t, personal, svc.withManagedAgentModelDefaults(ctx, personal), "administrator's personal agent remains configurable")
}

func TestManagedKnowledgeBaseDefaultsAndExistingEmbeddingProtection(t *testing.T) {
	ctx := managedMemberContext(t)
	models := &stubModelService{availableModels: []*types.Model{managedTestModel("chat", types.ModelTypeKnowledgeQA), managedTestModel("embed-new", types.ModelTypeEmbedding)}}
	svc := &knowledgeBaseService{repo: newFakeKBRepo(), modelService: models}
	kb, err := svc.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "Project knowledge"})
	require.NoError(t, err)
	assert.Equal(t, "chat", kb.SummaryModelID)
	assert.Equal(t, "embed-new", kb.EmbeddingModelID)
	_, err = svc.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "Bad override", EmbeddingModelID: "private"})
	require.ErrorContains(t, err, "默认模型")
	// Simulate a populated existing KB whose index was built with embed-old.
	kb.EmbeddingModelID = "embed-old"
	for _, id := range []string{"", "embed-old"} {
		cfg := &types.KnowledgeBaseConfig{EmbeddingModelID: &id}
		require.NoError(t, svc.normalizeManagedKnowledgeBaseUpdate(ctx, kb, cfg, nil))
		assert.Nil(t, cfg.EmbeddingModelID)
		assert.Equal(t, "embed-old", kb.EmbeddingModelID)
	}
	changed := "embed-new"
	require.Error(t, svc.normalizeManagedKnowledgeBaseUpdate(ctx, kb, &types.KnowledgeBaseConfig{EmbeddingModelID: &changed}, nil))
	assert.Equal(t, "embed-old", kb.EmbeddingModelID)
	_, err = svc.CreateKnowledgeBase(ctx, &types.KnowledgeBase{Name: "Inline model", VLMConfig: types.VLMConfig{BaseURL: "https://external.invalid", APIKey: "not-a-real-key"}})
	require.ErrorContains(t, err, "管理员")
}

func TestManagedAgentRuntimeIgnoresSavedAdvancedModelParameters(t *testing.T) {
	ctx := managedMemberContext(t)
	svc := &sessionService{cfg: &config.Config{Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{Temperature: 0.6, MaxCompletionTokens: 4096}}, Agent: &config.AgentConfig{LLMCallTimeout: 120}}, modelService: &stubModelService{}}
	cfg := types.CustomAgentConfig{Temperature: 1.9, MaxCompletionTokens: 99999, ReasoningEffort: "max", LLMCallTimeout: 9999, MaxIterations: -1, ModelID: "private", SandboxConfigID: "private-sandbox", SystemPrompt: "Keep this"}
	svc.applyManagedAgentModelDefaults(ctx, &cfg)
	assert.Equal(t, 0.6, cfg.Temperature)
	assert.Equal(t, 4096, cfg.MaxCompletionTokens)
	assert.Equal(t, 120, cfg.LLMCallTimeout)
	assert.Equal(t, 10, cfg.MaxIterations)
	assert.Empty(t, cfg.ModelID)
	assert.Empty(t, cfg.SandboxConfigID)
	assert.Empty(t, cfg.ReasoningEffort)
	assert.Equal(t, "Keep this", cfg.SystemPrompt)
}

func TestManagedKnowledgeBaseRuntimeBindingsCannotBeOverridden(t *testing.T) {
	ctx := managedMemberContext(t)
	svc := &knowledgeBaseService{}
	oldBackend, newBackend := "existing-files", "private-files"
	kb := &types.KnowledgeBase{StorageBackendID: &oldBackend, ChunkingConfig: types.ChunkingConfig{ParserEngineRules: []types.ParserEngineRule{{FileTypes: []string{"pdf"}, Engine: "docreader"}}}}
	cfg := &types.KnowledgeBaseConfig{StorageBackendID: &oldBackend}
	require.NoError(t, svc.normalizeManagedKnowledgeBaseUpdate(ctx, kb, cfg, nil))
	assert.Nil(t, cfg.StorageBackendID)
	assert.Equal(t, kb.ChunkingConfig.ParserEngineRules, cfg.ChunkingConfig.ParserEngineRules)
	require.Error(t, svc.normalizeManagedKnowledgeBaseUpdate(ctx, kb, &types.KnowledgeBaseConfig{StorageBackendID: &newBackend}, nil))
	require.Error(t, svc.normalizeManagedKnowledgeBaseUpdate(ctx, kb, &types.KnowledgeBaseConfig{ChunkingConfig: types.ChunkingConfig{ParserEngineRules: []types.ParserEngineRule{{Engine: "private-parser"}}}}, nil))
	vectorStore := "private-vector-store"
	require.Error(t, svc.applyManagedKnowledgeBaseModels(ctx, &types.KnowledgeBase{VectorStoreID: &vectorStore}))
	require.Error(t, svc.applyManagedKnowledgeBaseModels(ctx, &types.KnowledgeBase{StorageConfig: types.StorageConfig{Endpoint: "https://external.invalid"}}))
}

func TestManagedSearchRerankUsesCentralDefaultWithoutClientOverrides(t *testing.T) {
	ctx := managedMemberContext(t)
	models := &stubModelService{availableModels: []*types.Model{managedTestModel("rerank-default", types.ModelTypeRerank)}}
	id, _, err := resolveRerankModelID(ctx, models, "", &types.RetrievalConfig{RerankModelID: "old-private"})
	require.NoError(t, err)
	assert.Equal(t, "rerank-default", id)
	_, _, err = resolveRerankModelID(ctx, models, "other-model", nil)
	require.Error(t, err)
	models.availableModels = nil
	id, _, err = resolveRerankModelID(ctx, models, "", nil)
	require.NoError(t, err)
	assert.Empty(t, id, "reranking remains optional")
}

func TestManagedDuplicateSettingsUsesNewDefaultsWithoutChangingSourceIndex(t *testing.T) {
	ctx := managedMemberContext(t)
	repo := newFakeKBRepo()
	source := &types.KnowledgeBase{ID: "source", TenantID: 22, Name: "Project knowledge", EmbeddingModelID: "embed-old", SummaryModelID: "chat-old"}
	repo.rows[source.ID] = source
	svc := &knowledgeBaseService{repo: repo, modelService: &stubModelService{availableModels: []*types.Model{managedTestModel("embed-new", types.ModelTypeEmbedding), managedTestModel("chat-new", types.ModelTypeKnowledgeQA)}}}
	copy, err := svc.DuplicateKnowledgeBase(ctx, source.ID)
	require.NoError(t, err)
	assert.Equal(t, "embed-new", copy.EmbeddingModelID)
	assert.Equal(t, "chat-new", copy.SummaryModelID)
	assert.Equal(t, "embed-old", source.EmbeddingModelID)
	assert.Equal(t, "chat-old", source.SummaryModelID)
}
