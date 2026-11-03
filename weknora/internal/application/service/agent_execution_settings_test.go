package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/agent/collaboration"
	"github.com/Tencent/WeKnora/internal/config"
	appruntime "github.com/Tencent/WeKnora/internal/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type executionSettingsRepository struct {
	interfaces.SystemSettingRepository
	rows       map[string]*types.SystemSetting
	writeCount int
}

func (r *executionSettingsRepository) Get(_ context.Context, key string) (*types.SystemSetting, error) {
	return r.rows[key], nil
}

func (r *executionSettingsRepository) List(context.Context) ([]*types.SystemSetting, error) {
	var rows []*types.SystemSetting
	for _, row := range r.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *executionSettingsRepository) Upsert(_ context.Context, row *types.SystemSetting) error {
	r.writeCount++
	r.rows[row.Key] = row
	return nil
}

type executionSettingsAudit struct {
	interfaces.AuditLogService
	rows []*types.AuditLog
}

func (a *executionSettingsAudit) Log(_ context.Context, row *types.AuditLog) error {
	a.rows = append(a.rows, row)
	return nil
}

func newExecutionSettingsTestService() (*systemSettingService, *executionSettingsRepository, *executionSettingsAudit) {
	repo := &executionSettingsRepository{rows: map[string]*types.SystemSetting{}}
	audit := &executionSettingsAudit{}
	return &systemSettingService{repo: repo, audit: audit, cache: map[string]*types.SystemSetting{}}, repo, audit
}

func TestAgentExecutionSettingsRegistryDefaultsValidationAndAudit(t *testing.T) {
	svc, repo, audit := newExecutionSettingsTestService()
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "platform-admin")
	rows, err := svc.List(ctx)
	require.NoError(t, err)
	for key, expected := range map[string]string{
		appruntime.AgentMaxIterationsKey: "50", appruntime.AgentCollaborationWaitTimeoutSecondsKey: "30",
	} {
		found := false
		for _, row := range rows {
			if row.Key == key {
				found = true
				require.Equal(t, expected, string(row.Value))
				require.Equal(t, "int", row.ValueType)
				require.Equal(t, "agent", row.Category)
				require.False(t, row.RequiresRestart)
			}
		}
		require.True(t, found, "new registry keys appear before any database migration/write")
	}
	require.Zero(t, repo.writeCount)
	for key, maximum := range map[string]int{
		appruntime.AgentMaxIterationsKey: 200, appruntime.AgentCollaborationWaitTimeoutSecondsKey: 60,
	} {
		for _, invalid := range []any{-1, 0, maximum + 1, 1.5, "50", true, nil} {
			writes := repo.writeCount
			_, err := svc.Update(ctx, key, invalid)
			require.Error(t, err, "%s must reject %v", key, invalid)
			require.Equal(t, writes, repo.writeCount)
		}
		for _, value := range []int{1, maximum} {
			row, err := svc.Update(ctx, key, float64(value))
			require.NoError(t, err)
			require.Equal(t, "platform-admin", row.LastModifiedBy)
			require.EqualValues(t, value, svc.GetInt(ctx, key, "", 0))
		}
	}
	require.Len(t, audit.rows, 4)
	for _, row := range audit.rows {
		require.Equal(t, "platform-admin", row.ActorUserID)
		require.Equal(t, types.AuditActionSystemSettingChanged, row.Action)
	}
}

func TestManagedExecutionSettingsApplyToAllUsersAndOnlyNewRuns(t *testing.T) {
	ctx := managedMemberContext(t)
	settings, _, _ := newExecutionSettingsTestService()
	svc := &sessionService{systemSettingService: settings, cfg: &config.Config{}, webSearchProviderRepo: &sharedAgentWebSearchRepo{}}
	for _, admin := range []bool{false, true} {
		actor := context.WithValue(ctx, types.SystemAdminContextKey, admin)
		for _, requested := range []int{-1, 0, 10, 99999} {
			req := &types.QARequest{Session: &types.Session{ID: "session", TenantID: 22},
				CustomAgent: &types.CustomAgent{TenantID: 22, Config: types.CustomAgentConfig{MaxIterations: requested}}}
			cfg, err := svc.buildAgentConfig(actor, req, &types.Tenant{ID: 22}, 22)
			require.NoError(t, err)
			require.Equal(t, 50, cfg.MaxIterations, "agent overrides do not bypass platform policy, admin=%v", admin)
			require.Equal(t, 30, cfg.CollaborationWaitTimeoutSeconds)
		}
	}
	started := &types.AgentConfig{}
	require.NoError(t, svc.applyAgentExecutionSettings(ctx, &types.QARequest{}, started))
	savedRequest := resumableAgentRequest(ctx, &types.QARequest{}, started)
	blob, err := json.Marshal(savedRequest)
	require.NoError(t, err)
	_, err = settings.Update(ctx, appruntime.AgentMaxIterationsKey, 90)
	require.NoError(t, err)
	_, err = settings.Update(ctx, appruntime.AgentCollaborationWaitTimeoutSecondsKey, 45)
	require.NoError(t, err)
	next := &types.AgentConfig{MaxIterations: -1}
	require.NoError(t, svc.applyAgentExecutionSettings(ctx, &types.QARequest{}, next))
	require.Equal(t, 90, next.MaxIterations)
	require.Equal(t, 45, next.CollaborationWaitTimeoutSeconds)
	require.Equal(t, 50, started.MaxIterations)
	var restoredRequest types.ResumableAgentRequest
	require.NoError(t, json.Unmarshal(blob, &restoredRequest))
	snapshot, err := json.Marshal(agent.RunSnapshot{Version: agent.RunSnapshotVersion, Request: blob})
	require.NoError(t, err)
	savedBudget, err := executionSettingsFromSnapshot(snapshot)
	require.NoError(t, err)
	require.Equal(t, restoredRequest.ExecutionSettings, savedBudget, "approval and automation snapshots retain the same budget")
	resumed := &types.AgentConfig{}
	require.NoError(t, svc.applyAgentExecutionSettings(ctx, &types.QARequest{
		Resume: &types.AgentResume{ExecutionSettings: restoredRequest.ExecutionSettings},
	}, resumed))
	require.Equal(t, *started, *resumed)

	// Explicit-agent children also pin the parent budget before queueing.
	savedChild, err := json.Marshal(delegatedConfig{ExecutionSettings: savedRequest.ExecutionSettings})
	require.NoError(t, err)
	b := &collaboration.Binding{Child: &collaboration.Work{Member: collaboration.Member{ConfigJSON: string(savedChild)}}}
	child := &types.AgentConfig{}
	require.NoError(t, svc.applyAgentExecutionSettings(collaboration.WithBinding(ctx, b), &types.QARequest{}, child))
	require.Equal(t, *started, *child)
}

func TestWorkspaceManagedAgentKeepsItsOwnIterationBudget(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "false")
	svc := &sessionService{}
	for _, iterations := range []int{-1, 10, 75} {
		cfg := &types.AgentConfig{MaxIterations: iterations}
		require.NoError(t, svc.applyAgentExecutionSettings(context.Background(), &types.QARequest{}, cfg))
		require.NoError(t, (&agentService{}).ValidateConfig(cfg))
		require.Equal(t, iterations, cfg.MaxIterations)
	}
	require.Error(t, (&agentService{}).ValidateConfig(&types.AgentConfig{MaxIterations: 101}),
		"workspace-managed installations retain their original validation ceiling")
}

func TestManagedExecutionSettingsMaximumPassesEngineValidation(t *testing.T) {
	ctx := managedMemberContext(t)
	settings, _, _ := newExecutionSettingsTestService()
	svc := &sessionService{systemSettingService: settings, cfg: &config.Config{}, webSearchProviderRepo: &sharedAgentWebSearchRepo{}}
	for _, iterations := range []int{1, 50, 200} {
		_, err := settings.Update(ctx, appruntime.AgentMaxIterationsKey, iterations)
		require.NoError(t, err)
		req := &types.QARequest{Session: &types.Session{ID: "session", TenantID: 22},
			CustomAgent: &types.CustomAgent{TenantID: 22, Config: types.CustomAgentConfig{MaxIterations: -1}}}
		cfg, err := svc.buildAgentConfig(ctx, req, &types.Tenant{ID: 22}, 22)
		require.NoError(t, err)
		require.NoError(t, (&agentService{}).ValidateConfig(cfg))
		require.Equal(t, iterations, cfg.MaxIterations)
	}
	require.Error(t, (&agentService{}).ValidateConfig(&types.AgentConfig{MaxIterations: 201}))
}
