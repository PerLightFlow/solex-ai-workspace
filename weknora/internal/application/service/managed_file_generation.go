package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/sandbox"
	"github.com/Tencent/WeKnora/internal/types"
)

const ManagedFileSandboxKey = "agent.file_generation_sandbox_id"

type sandboxSettingReader interface {
	GetString(context.Context, string, string, string) string
}

// Only server-side policy supplies a cross-workspace backend. The request's
// data tenant is never replaced by the configuration owner.
type managedFileSandboxPolicy interface {
	ManagedFileSandbox(context.Context) (SandboxPin, error)
}

func (s *TenantSandboxConfigService) WithManagedRuntimeSettings(reader sandboxSettingReader) *TenantSandboxConfigService {
	s.managedSettings = reader
	return s
}

func (s *TenantSandboxConfigService) isManagedFileDefault(ctx context.Context, tenant uint64, id string) bool {
	return types.ManagedRuntimeEnabled() && tenant == types.ManagedRuntimeTenantID() &&
		s.managedSettings != nil && id != "" && s.managedSettings.GetString(ctx, ManagedFileSandboxKey, "", "") == id
}

// ValidateManagedFileSandbox rejects templates that would share a user's
// writable files or administrator environment secrets with other accounts.
func ValidateManagedFileSandbox(entity *types.TenantSandboxConfigEntity) error {
	if entity == nil || entity.Config == nil || entity.TenantID != types.ManagedRuntimeTenantID() ||
		entity.SandboxType != string(sandbox.SandboxTypeDocker) || entity.Config.Docker == nil {
		return fmt.Errorf("请选择系统配置空间中的 Docker 沙箱")
	}
	cfg := entity.Config
	if cfg.AllocationMode != "" && cfg.AllocationMode != "session" {
		return fmt.Errorf("统一文件生成必须按会话分配沙箱")
	}
	if cfg.VolumeMount != nil || len(cfg.EnvVars) != 0 {
		return fmt.Errorf("统一文件生成沙箱不能配置共享卷或公共环境变量")
	}
	return nil
}

func (s *TenantSandboxConfigService) ManagedFileSandbox(ctx context.Context) (SandboxPin, error) {
	if !types.ManagedRuntimeEnabled() || s == nil || s.managedSettings == nil {
		return SandboxPin{}, nil
	}
	id := strings.TrimSpace(s.managedSettings.GetString(ctx, ManagedFileSandboxKey, "", ""))
	if id == "" {
		return SandboxPin{}, nil
	}
	owner := types.ManagedRuntimeTenantID()
	if owner == 0 || s.repo == nil {
		return SandboxPin{}, fmt.Errorf("系统文件生成环境尚未配置完整")
	}
	entity, err := s.repo.GetByID(ctx, owner, id)
	if err != nil {
		return SandboxPin{}, err
	}
	if err := ValidateManagedFileSandbox(entity); err != nil {
		return SandboxPin{}, err
	}
	return SandboxPin{TenantID: owner, ConfigID: id}, nil
}

func managedFileSandbox(ctx context.Context, policy WorkspaceSandboxPolicy) (SandboxPin, error) {
	if !types.ManagedRuntimeEnabled() {
		return SandboxPin{}, nil
	}
	if p, ok := policy.(managedFileSandboxPolicy); ok {
		return p.ManagedFileSandbox(ctx)
	}
	return SandboxPin{}, nil
}

// A pinned central backend remains executable when the administrator chooses
// another default. Turning off the default removes the managed shell grant;
// the existing Docker kill switch can also stop all Docker execution.
func managedFileShellAllowed(ctx context.Context, policy WorkspaceSandboxPolicy, pin SandboxPin) bool {
	current, err := managedFileSandbox(ctx, policy)
	return err == nil && !current.IsZero() && !pin.IsZero() &&
		pin.TenantID == types.ManagedRuntimeTenantID()
}
