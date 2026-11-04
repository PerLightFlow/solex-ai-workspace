package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/sandbox/allocation"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type managedFilePolicyStub struct {
	pin      SandboxPin
	disabled uint64
}

func (p *managedFilePolicyStub) WorkspaceScriptsDisabled(_ context.Context, id uint64) (bool, error) {
	return p.disabled != 0 && p.disabled == id, nil
}
func (p *managedFilePolicyStub) ManagedFileSandbox(context.Context) (SandboxPin, error) {
	return p.pin, nil
}

func TestManagedFileGenerationPinsCentralBackendWithoutChangingDataTenant(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	ctx := types.WithSandboxTenantID(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7)), 7)
	policy := &managedFilePolicyStub{pin: SandboxPin{TenantID: lenderTenant, ConfigID: lentSandboxConfig}}
	resolver := &lendingResolver{mgr: &destroyRecordingManager{}}
	pinner := NewSessionSandboxPinner(newPinTestDB(t))
	mgr, pin, err := resolveSandboxForExecution(ctx, resolver, nil, pinner, 7, "s-1", "", policy)
	require.NoError(t, err)
	require.Equal(t, policy.pin, pin)
	require.NotEqual(t, sandbox.SandboxTypeDisabled, mgr.GetType())
	owner, _ := types.TenantIDFromContext(ctx)
	require.Equal(t, uint64(7), owner)
	stored, err := pinner.Read(ctx, "s-1")
	require.NoError(t, err)
	require.Equal(t, policy.pin, stored)
	// Changing the default must not move an existing conversation's files.
	policy.pin.ConfigID = "different-backend"
	_, resumed, err := resolveSandboxForExecution(ctx, resolver, nil, pinner, 7, "s-1", "", policy)
	require.NoError(t, err)
	require.Equal(t, stored, resumed)
}

func TestManagedFileGenerationOffersExecutableToolsWithSkillsOff(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	model := &fakeAgentChatModel{}
	svc := &agentService{
		sandboxPolicy: &managedFilePolicyStub{pin: SandboxPin{TenantID: lenderTenant, ConfigID: lentSandboxConfig}},
		sandboxResolver: &lendingResolver{mgr: &capableManager{typ: sandbox.SandboxTypeDocker,
			shell: &stubShellExecutor{}, files: stubSessionFileStore{}}},
	}
	cfg := &types.AgentConfig{SkillsEnabled: false, MaxCompletionTokens: 1024}
	engine, err := svc.CreateAgentEngine(ctx, cfg, model, nil, nil, "s-1", "m-1")
	require.NoError(t, err)
	_, err = engine.Execute(ctx, "s-1", "m-1", "Create a workbook", nil)
	require.NoError(t, err)
	require.True(t, toolOffered(model.lastToolNames, tools.ToolShellExec))
	require.True(t, toolOffered(model.lastToolNames, tools.ToolReadFile))
	require.True(t, toolOffered(model.lastToolNames, tools.ToolWriteSandboxFile))
	require.Equal(t, 8192, model.lastCompletionBudget, "inherited files get the minimum file budget even without a private config ID")
	require.Empty(t, cfg.SandboxConfigID)
	require.False(t, toolOffered(model.lastToolNames, tools.LegacyToolReadSkill))
}

func TestManagedFileGenerationHonorsSessionWorkspaceDisable(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	ctx := types.WithSandboxTenantID(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7)), 7)
	policy := &managedFilePolicyStub{pin: SandboxPin{TenantID: lenderTenant, ConfigID: lentSandboxConfig}, disabled: 7}
	pinner := NewSessionSandboxPinner(newPinTestDB(t))
	_, err := pinner.Pin(ctx, "s-1", policy.pin)
	require.NoError(t, err)
	mgr, _, err := resolveSandboxForExecution(ctx, &lendingResolver{mgr: &destroyRecordingManager{}}, nil, pinner, 7, "s-1", "", policy)
	require.NoError(t, err)
	require.Equal(t, sandbox.SandboxTypeDisabled, mgr.GetType())
}

func TestManagedFileGenerationChildKeepsBackendButHasOwnSession(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	_, agents, _, ctx := collaborationService(t)
	ctx = context.WithValue(ctx, childSourceKey{}, childSource{Agent: agents.agent, ModelID: "model",
		SandboxConfigID: lentSandboxConfig, SandboxConfigTenantID: lenderTenant})
	raw, err := inheritedChildSnapshot(ctx, &types.AgentConfig{}, nil)
	require.NoError(t, err)
	var saved delegatedConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &saved))
	require.Equal(t, lenderTenant, saved.Inherited.SandboxConfigTenantID)
	db := newPinTestDB(t)
	require.NoError(t, db.Model(&types.Session{}).Create(map[string]any{"id": "child", "tenant_id": uint64(7)}).Error)
	pinner := NewSessionSandboxPinner(db)
	original := SandboxPin{TenantID: lenderTenant, ConfigID: lentSandboxConfig}
	_, err = pinner.Pin(ctx, "s-1", original)
	require.NoError(t, err)
	svc := &sessionService{sandboxPinner: pinner,
		sandboxPolicy:   &managedFilePolicyStub{pin: SandboxPin{TenantID: lenderTenant, ConfigID: "new-default"}},
		sandboxResolver: &lendingResolver{mgr: &destroyRecordingManager{}}}
	ctx = context.WithValue(ctx, types.SessionIDContextKey, "child")
	_, err = svc.restoreInheritedConfig(ctx, &saved, &types.Tenant{ID: 7})
	require.NoError(t, err)
	child, err := pinner.Read(ctx, "child")
	require.NoError(t, err)
	require.Equal(t, original, child)
	parent, err := pinner.Read(ctx, "s-1")
	require.NoError(t, err)
	require.Equal(t, original, parent)
	saved.Inherited.SandboxConfigTenantID = 123
	_, err = svc.restoreInheritedConfig(ctx, &saved, &types.Tenant{ID: 7})
	require.ErrorContains(t, err, "invalid inherited sandbox configuration owner")
}

func TestManagedFileGenerationTemplateMustBeIsolatedAndCentrallyOwned(t *testing.T) {
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	valid := func() *types.TenantSandboxConfigEntity {
		return &types.TenantSandboxConfigEntity{TenantID: 99, SandboxType: "docker",
			Config: &types.TenantSandboxConfig{SandboxType: "docker", AllocationMode: "session",
				Docker: &types.DockerSandboxConfig{Image: "solex/file-sandbox:test"}}}
	}
	require.NoError(t, ValidateManagedFileSandbox(valid()))
	for _, mutate := range []func(*types.TenantSandboxConfigEntity){
		func(e *types.TenantSandboxConfigEntity) { e.TenantID = 7 },
		func(e *types.TenantSandboxConfigEntity) { e.SandboxType = "e2b" },
		func(e *types.TenantSandboxConfigEntity) { e.Config.AllocationMode = "user" },
		func(e *types.TenantSandboxConfigEntity) {
			e.Config.VolumeMount = &types.VolumeMountConfig{Enabled: true}
		},
		func(e *types.TenantSandboxConfigEntity) { e.Config.EnvVars = map[string]string{"TOKEN": "private"} },
	} {
		entity := valid()
		mutate(entity)
		require.Error(t, ValidateManagedFileSandbox(entity))
	}
}

func TestManagedFileGenerationDisabledDefaultRevokesAutomaticShellGrant(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	policy := &managedFilePolicyStub{}
	require.False(t, managedFileShellAllowed(context.Background(), policy,
		SandboxPin{TenantID: lenderTenant, ConfigID: lentSandboxConfig}))
	policy.pin = SandboxPin{TenantID: lenderTenant, ConfigID: lentSandboxConfig}
	require.False(t, managedFileShellAllowed(context.Background(), policy,
		SandboxPin{TenantID: 123, ConfigID: lentSandboxConfig}))
	t.Setenv("SOLEX_MANAGED_RUNTIME", "false")
	require.False(t, managedFileShellAllowed(context.Background(), policy, policy.pin))
}

type managedFileSettingStub string

func (s managedFileSettingStub) GetString(context.Context, string, string, string) string {
	return string(s)
}

func TestManagedFileGenerationProtectsTheSelectedConfiguration(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	t.Setenv("SOLEX_RUNTIME_TENANT_ID", "99")
	t.Setenv("WEKNORA_SANDBOX_DOCKER_ENABLED", "true")
	allocation.SetUserModeAvailable(true)
	t.Cleanup(func() { allocation.SetUserModeAvailable(false) })
	repo := &fakeConfigRepo{entity: &types.TenantSandboxConfigEntity{ID: lentSandboxConfig, TenantID: lenderTenant,
		SandboxType: "docker", Config: &types.TenantSandboxConfig{SandboxType: "docker",
			Docker: &types.DockerSandboxConfig{Image: "solex/file-sandbox:test"}}}}
	svc := (&TenantSandboxConfigService{repo: repo}).WithManagedRuntimeSettings(managedFileSettingStub(lentSandboxConfig))
	err := svc.Delete(context.Background(), lenderTenant, lentSandboxConfig, true)
	require.ErrorContains(t, err, "默认文件生成环境")
	require.False(t, repo.deleted)
	_, err = svc.Update(context.Background(), lenderTenant, lentSandboxConfig, UpdateSandboxConfigInput{
		Config: &types.TenantSandboxConfig{SandboxType: "docker", AllocationMode: "user",
			Docker: &types.DockerSandboxConfig{Image: "solex/file-sandbox:test"}},
	})
	require.ErrorContains(t, err, "按会话分配沙箱")
	require.Nil(t, repo.updated)
}
