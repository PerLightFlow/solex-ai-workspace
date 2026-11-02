package types

import (
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SystemBuiltinAgentPrefix is reserved even when managed mode is disabled.
// User-created agents must never impersonate an internal privileged identity.
const SystemBuiltinAgentPrefix = "builtin-"

func IsSystemBuiltinAgentID(id string) bool {
	return strings.HasPrefix(id, SystemBuiltinAgentPrefix) || IsBuiltinAgentID(id)
}

// AllBuiltinAgentIDs includes internal assistants omitted from the public picker.
func AllBuiltinAgentIDs() []string {
	ids := make([]string, 0, len(BuiltinAgentRegistry))
	for id := range BuiltinAgentRegistry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SystemBuiltinAgent is a platform definition, never a resource-owning tenant.
// Deleted rows are retained: seeding must not resurrect removed YAML defaults.
type SystemBuiltinAgent struct {
	ID          string            `gorm:"type:varchar(36);primaryKey"`
	Name        string            `gorm:"type:varchar(255);not null"`
	Description string            `gorm:"type:text"`
	Avatar      string            `gorm:"type:varchar(64)"`
	Config      CustomAgentConfig `gorm:"type:json"`
	IsInternal  bool
	SortOrder   int
	Customized  bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (SystemBuiltinAgent) TableName() string { return "system_builtin_agents" }

func (a *SystemBuiltinAgent) ForTenant(tenantID uint64) *CustomAgent {
	return &CustomAgent{ID: a.ID, Name: a.Name, Description: a.Description,
		Avatar: a.Avatar, Config: a.Config, IsBuiltin: true, IsInternal: a.IsInternal,
		TenantID: tenantID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}
