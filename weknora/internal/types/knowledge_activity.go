package types

import "time"

// KnowledgeActivity is the safe document-level projection of an audit event.
// It deliberately excludes actor IDs, credentials, request details, and values
// before/after a change. Readers of a document may see this projection even
// when they cannot inspect the owning workspace's administrative audit feed.
type KnowledgeActivity struct {
	ID            uint64      `json:"id"`
	Action        AuditAction `json:"action"`
	ActorName     string      `json:"actor_name"`
	ActorKind     string      `json:"actor_kind"`
	CreatedAt     time.Time   `json:"created_at"`
	ChangedFields []string    `json:"changed_fields"`
}
