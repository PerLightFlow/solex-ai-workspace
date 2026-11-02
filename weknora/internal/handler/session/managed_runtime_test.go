package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type managedAttachmentModels struct {
	interfaces.ModelService
	models []*types.Model
}

func (m managedAttachmentModels) ListModels(context.Context) ([]*types.Model, error) {
	return m.models, nil
}

func TestManagedAttachmentUsesCentralVisionAndAudioDefaults(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	h := &Handler{modelService: managedAttachmentModels{models: []*types.Model{
		{ID: "shared-vision", Type: types.ModelTypeVLLM, Status: types.ModelStatusActive, IsBuiltin: true, IsDefault: true},
		{ID: "shared-audio", Type: types.ModelTypeASR, Status: types.ModelStatusActive, IsBuiltin: true, DefaultFor: []string{string(types.ModelTypeASR)}},
		{ID: "private", Type: types.ModelTypeASR, Status: types.ModelStatusActive, IsDefault: true},
	}}}
	agent := &types.CustomAgent{TenantID: 7, Config: types.CustomAgentConfig{ImageUploadEnabled: true, AudioUploadEnabled: true, ASRModelID: "private", VLMModelID: "private", ImageStorageProvider: "cos", ChatParserEngineRules: []types.ParserEngineRule{{Engine: "custom"}}}}
	got := h.withManagedAttachmentModels(ctx, agent)
	assert.Equal(t, "shared-vision", got.Config.VLMModelID)
	assert.Equal(t, "shared-audio", got.Config.ASRModelID)
	assert.Empty(t, got.Config.ChatParserEngineRules)
	assert.Empty(t, got.Config.ImageStorageProvider)
	assert.Equal(t, uint64(7), got.TenantID)
	assert.Equal(t, "private", agent.Config.ASRModelID, "runtime overlay must not mutate stored config")
	admin := context.WithValue(ctx, types.SystemAdminContextKey, true)
	assert.Same(t, agent, h.withManagedAttachmentModels(admin, agent))
}

func TestManagedTemporaryDocumentCannotOverrideParser(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	documents := &engineTestDocuments{}
	h := &Handler{sessionService: engineTestSessions{}, temporaryDocuments: documents, customAgentService: &resolveOwnAgentStub{agent: &types.CustomAgent{ID: "own-agent", TenantID: 7, Config: types.CustomAgentConfig{ChatParserEngineRules: []types.ParserEngineRule{{FileTypes: []string{"pdf"}, Engine: "custom"}}}}}}
	uploadWithEngine(t, h, "own-agent")
	require.Empty(t, documents.options.ParserEngine)
}

func TestAdminGlobalAgentAttachmentsInheritSystemModels(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	h := &Handler{modelService: managedAttachmentModels{models: []*types.Model{
		{ID: "shared-chat", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive, IsBuiltin: true, IsDefault: true},
		{ID: "shared-vision", Type: types.ModelTypeVLLM, Status: types.ModelStatusActive, IsBuiltin: true, IsDefault: true},
		{ID: "shared-audio", Type: types.ModelTypeASR, Status: types.ModelStatusActive, IsBuiltin: true, IsDefault: true},
	}}}
	for _, id := range []string{types.BuiltinQuickAnswerID, "builtin-123456789012345678901234"} {
		agent := &types.CustomAgent{ID: id, IsBuiltin: true, TenantID: 7}
		got := h.withManagedAttachmentModels(ctx, agent)
		assert.Equal(t, "shared-chat", got.Config.ModelID)
		assert.Equal(t, "shared-vision", got.Config.VLMModelID)
		assert.Equal(t, "shared-audio", got.Config.ASRModelID)
		assert.Equal(t, uint64(7), got.TenantID)
		assert.Empty(t, agent.Config.ASRModelID, "global definition must remain unbound")
	}
	t.Setenv("SOLEX_MANAGED_RUNTIME", "false")
	agent := &types.CustomAgent{ID: types.BuiltinQuickAnswerID, IsBuiltin: true, TenantID: 7}
	assert.Same(t, agent, h.withManagedAttachmentModels(ctx, agent))
}
