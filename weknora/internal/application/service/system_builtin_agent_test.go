package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func builtinCatalogTestService(t *testing.T) (*customAgentService, *gorm.DB, context.Context, context.Context) {
	t.Helper()
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	require.NoError(t, types.LoadBuiltinAgentsConfig(filepath.Join("..", "..", "..", "config")))
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "catalog.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.SystemBuiltinAgent{}, &types.CustomAgent{}))
	svc := &customAgentService{repo: repository.NewCustomAgentRepository(db)}
	member := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(22))
	member = context.WithValue(member, types.TenantRoleContextKey, types.TenantRoleOwner)
	admin := context.WithValue(member, types.TenantIDContextKey, uint64(10000))
	admin = context.WithValue(admin, types.SystemAdminContextKey, true)
	return svc, db, admin, member
}

func TestSystemBuiltinCatalogGlobalEditsAndDeletionSurviveRestart(t *testing.T) {
	svc, db, admin, member := builtinCatalogTestService(t)
	// Old workspace overrides must not override the platform definition.
	require.NoError(t, db.Create(&types.CustomAgent{ID: types.BuiltinSmartReasoningID, TenantID: 22,
		Name: "private override", IsBuiltin: true, Config: types.CustomAgentConfig{KnowledgeBases: []string{"private-kb"}}}).Error)
	seed, err := svc.GetAgentByID(member, types.BuiltinSmartReasoningID)
	require.NoError(t, err)
	require.NotEqual(t, "private override", seed.Name)
	require.Empty(t, seed.Config.KnowledgeBases)
	seed.Name, seed.Description, seed.Avatar = "Shared name", "Shared description", "S"
	seed.Config.SystemPrompt = "Global prompt"
	updated, err := svc.UpdateSystemBuiltinAgent(admin, seed, &seed.Avatar)
	require.NoError(t, err)
	require.Equal(t, "Shared name", updated.Name, "YAML localization must not undo an administrator edit")
	other := context.WithValue(member, types.TenantIDContextKey, uint64(33))
	for _, ctx := range []context.Context{member, other} {
		got, err := svc.GetAgentByID(ctx, seed.ID)
		require.NoError(t, err)
		require.Equal(t, "Shared name", got.Name)
		require.Equal(t, "Global prompt", got.Config.SystemPrompt)
		require.Equal(t, types.MustTenantIDFromContext(ctx), got.TenantID)
		got, err = svc.GetAgentByIDAndTenant(ctx, seed.ID, 10000)
		require.NoError(t, err)
		require.Equal(t, types.MustTenantIDFromContext(ctx), got.TenantID, "explicit source must not grant administrator resources")
	}
	require.NoError(t, svc.DeleteSystemBuiltinAgent(admin, seed.ID))
	// A fresh process seeds again. Its INSERT must conflict with the tombstone.
	restarted := &customAgentService{repo: repository.NewCustomAgentRepository(db)}
	_, err = restarted.GetAgentByID(member, seed.ID)
	require.ErrorIs(t, err, ErrAgentNotFound)
	_, err = restarted.GetAgentByIDAndTenant(member, seed.ID, 10000)
	require.ErrorIs(t, err, ErrAgentNotFound)
	_, err = restarted.CopyAgent(member, seed.ID)
	require.ErrorIs(t, err, ErrAgentNotFound)
	listed, err := restarted.ListAgents(member)
	require.NoError(t, err)
	for _, a := range listed {
		require.NotEqual(t, seed.ID, a.ID)
	}
	var tombstones int64
	require.NoError(t, db.Unscoped().Model(&types.SystemBuiltinAgent{}).Where("id = ? AND deleted_at IS NOT NULL", seed.ID).Count(&tombstones).Error)
	require.EqualValues(t, 1, tombstones)
}

func TestSystemBuiltinCatalogNewAgentsAndPersonalAgentsRemainSeparate(t *testing.T) {
	svc, _, admin, member := builtinCatalogTestService(t)
	created, err := svc.CreateSystemBuiltinAgent(admin, &types.CustomAgent{
		ID: types.BuiltinWeKnoraAssistantID, Name: "Company helper", Config: types.CustomAgentConfig{KBSelectionMode: "all"}})
	require.NoError(t, err)
	require.True(t, types.IsSystemBuiltinAgentID(created.ID))
	require.NotEqual(t, types.BuiltinWeKnoraAssistantID, created.ID)
	require.False(t, types.GrantsOperationCatalog(created), "new IDs cannot forge internal operation privileges")
	require.False(t, created.IsInternal)
	got, err := svc.GetAgentByID(member, created.ID)
	require.NoError(t, err)
	require.EqualValues(t, 22, got.TenantID)
	private, err := svc.CreateAgent(member, &types.CustomAgent{Name: "Personal"})
	require.NoError(t, err)
	listed, err := svc.ListAgents(member)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, a := range listed {
		ids[a.ID] = true
		require.False(t, a.IsInternal)
	}
	require.True(t, ids[private.ID])
	require.True(t, ids[created.ID])
	all, err := svc.ListSystemBuiltinAgents(admin)
	require.NoError(t, err)
	foundInternal := false
	for _, a := range all {
		if a.ID == types.BuiltinWeKnoraAssistantID {
			foundInternal = a.IsInternal
		}
	}
	require.True(t, foundInternal)
}

func TestSystemBuiltinCatalogOnlyRealSystemAdminCanManage(t *testing.T) {
	svc, _, admin, member := builtinCatalogTestService(t)
	for _, ctx := range []context.Context{member, types.WithTenantAPIKeyScope(admin, types.TenantAPIKeyScope{ScopeType: types.APIKeyScopePlatform})} {
		_, err := svc.CreateSystemBuiltinAgent(ctx, &types.CustomAgent{Name: "x"})
		var ae *apperrors.AppError
		require.ErrorAs(t, err, &ae)
		_, err = svc.ListSystemBuiltinAgents(ctx)
		require.Error(t, err)
		_, err = svc.GetSystemBuiltinAgent(ctx, types.BuiltinSmartReasoningID)
		require.Error(t, err)
		_, err = svc.UpdateSystemBuiltinAgent(ctx, &types.CustomAgent{ID: types.BuiltinSmartReasoningID, Name: "x"}, nil)
		require.Error(t, err)
		require.Error(t, svc.DeleteSystemBuiltinAgent(ctx, types.BuiltinSmartReasoningID))
	}
	// Even administrators must use the global API; personal CRUD cannot shadow it.
	_, err := svc.UpdateAgent(admin, &types.CustomAgent{ID: types.BuiltinSmartReasoningID}, nil)
	require.ErrorIs(t, err, ErrCannotModifyBuiltin)
	require.ErrorIs(t, svc.DeleteAgent(member, types.BuiltinSmartReasoningID), ErrCannotDeleteBuiltin)
	for _, enabled := range []string{"true", "false"} {
		t.Setenv("SOLEX_MANAGED_RUNTIME", enabled)
		_, err := svc.CreateAgent(member, &types.CustomAgent{ID: "builtin-forged", Name: "forged"})
		require.ErrorIs(t, err, ErrCannotModifyBuiltin)
	}
}

func TestSystemBuiltinCatalogRejectsPrivateResourceBindings(t *testing.T) {
	svc, _, admin, _ := builtinCatalogTestService(t)
	for _, cfg := range []types.CustomAgentConfig{
		{KBSelectionMode: "selected"}, {KnowledgeBases: []string{"private"}},
		{MCPServices: []string{"private"}}, {MCPSelectionMode: "selected"},
		{SelectedSkills: []string{"private"}}, {SkillsSelectionMode: "selected"},
		{ModelID: "private"}, {RerankModelID: "private"}, {VLMModelID: "private"}, {ASRModelID: "private"}, {QueryUnderstandModelID: "private"},
		{SandboxConfigID: "private"}, {WebSearchProviderID: "private"}, {ImageStorageProvider: "s3"},
		{ChatParserEngineRules: []types.ParserEngineRule{{}}},
		{QuestionSuggestions: &types.QuestionSuggestionConfig{FollowUps: types.FollowUpSuggestionConfig{ModelID: "private"}}},
	} {
		_, err := svc.CreateSystemBuiltinAgent(admin, &types.CustomAgent{Name: "Unsafe", Config: cfg})
		require.Error(t, err)
	}
	seed, err := svc.GetSystemBuiltinAgent(admin, types.BuiltinQuickAnswerID)
	require.NoError(t, err)
	seed.Config.AgentMode = types.AgentModeSmartReasoning
	_, err = svc.UpdateSystemBuiltinAgent(admin, seed, nil)
	require.ErrorContains(t, err, "运行模式")
}
