package types

import (
	"os"
	"strings"
	"time"
)

// UserSpacesEnabled is a deployment gate. Enable only after the member migration
// and its permission comparison have succeeded; the legacy endpoints are then
// read-only/disabled, not a second way to grant access.
func UserSpacesEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("SOLEX_USER_SPACES")), "true")
}

// SpaceMember binds collaboration to an account, never to all users of a tenant.
type SpaceMember struct {
	SpaceID  string    `json:"space_id" gorm:"primaryKey;type:varchar(36)"`
	UserID   string    `json:"user_id" gorm:"primaryKey;type:varchar(36)"`
	Role     string    `json:"role" gorm:"type:varchar(16);not null"`
	JoinedAt time.Time `json:"joined_at"`
}

func (SpaceMember) TableName() string { return "solex_space_members" }

type SpaceInvitation struct {
	SpaceName  string     `json:"space_name,omitempty" gorm:"-"`
	ID         string     `json:"id" gorm:"primaryKey;type:varchar(36)"`
	SpaceID    string     `json:"space_id" gorm:"type:varchar(36);index"`
	Email      string     `json:"email" gorm:"type:varchar(255);index"`
	InvitedBy  string     `json:"invited_by" gorm:"type:varchar(36)"`
	Status     string     `json:"status" gorm:"type:varchar(16)"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

func (SpaceInvitation) TableName() string { return "solex_space_invitations" }

// SpaceResource records resource ownership independently of physical storage.
// IDs and storage/index bindings stay stable during an ownership transfer.
type SpaceResource struct {
	Kind           string    `json:"kind" gorm:"primaryKey;type:varchar(24)"`
	ResourceID     string    `json:"resource_id" gorm:"primaryKey;type:varchar(36)"`
	SourceTenantID uint64    `json:"source_tenant_id" gorm:"primaryKey"`
	OwnerUserID    string    `json:"owner_user_id" gorm:"type:varchar(36);index"`
	OwnerSpaceID   string    `json:"owner_space_id" gorm:"type:varchar(36);index"`
	CreatedBy      string    `json:"created_by" gorm:"type:varchar(36)"`
	CreatedAt      time.Time `json:"created_at"`
}

func (SpaceResource) TableName() string { return "solex_space_resources" }

type SpaceGrant struct {
	ID             string    `json:"id" gorm:"primaryKey;type:varchar(36)"`
	SpaceID        string    `json:"space_id" gorm:"uniqueIndex:space_resource_grant;type:varchar(36)"`
	Kind           string    `json:"kind" gorm:"uniqueIndex:space_resource_grant;type:varchar(24)"`
	ResourceID     string    `json:"resource_id" gorm:"uniqueIndex:space_resource_grant;type:varchar(36)"`
	SourceTenantID uint64    `json:"source_tenant_id" gorm:"uniqueIndex:space_resource_grant"`
	AllowEdit      bool      `json:"allow_edit"`
	AllowDownload  bool      `json:"allow_download"`
	SharedBy       string    `json:"shared_by" gorm:"type:varchar(36)"`
	HistoryFrom    time.Time `json:"history_from"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (SpaceGrant) TableName() string { return "solex_space_grants" }

type SpaceEvent struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	SpaceID   string    `json:"space_id" gorm:"type:varchar(36);index"`
	ActorID   string    `json:"actor_id" gorm:"type:varchar(36)"`
	Action    string    `json:"action" gorm:"type:varchar(64)"`
	Subject   string    `json:"subject" gorm:"type:varchar(255)"`
	CreatedAt time.Time `json:"created_at"`
}

func (SpaceEvent) TableName() string { return "solex_space_events" }
