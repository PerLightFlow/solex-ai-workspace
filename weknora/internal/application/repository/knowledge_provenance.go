package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

var knowledgeProvenanceColumns = []string{"created_by", "created_by_name", "last_modified_by", "last_modified_by_name", "last_modified_at"}

func (r *knowledgeRepository) knowledgeActor(ctx context.Context) (string, string) {
	id, name := types.KnowledgeActor(ctx)
	if id != "" && name == "" {
		// Async user-initiated moves carry a verified user ID, not a User object.
		var user struct{ Username string }
		if err := r.db.WithContext(ctx).Model(&types.User{}).Select("username").Where("id = ?", id).Take(&user).Error; err == nil {
			name = types.KnowledgeActorName(user.Username)
		}
	}
	return id, name
}

func (r *knowledgeRepository) setKnowledgeCreation(ctx context.Context, k *types.Knowledge) {
	id, name := r.knowledgeActor(ctx)
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	k.CreatedAt = k.CreatedAt.UTC().Truncate(time.Microsecond)
	k.CreatedBy, k.CreatedByName = id, name
	k.LastModifiedBy, k.LastModifiedByName, k.LastModifiedAt = "", "", nil
	if id != "" {
		now := k.CreatedAt
		k.LastModifiedBy, k.LastModifiedByName, k.LastModifiedAt = id, name, &now
	}
}

func (r *knowledgeRepository) knowledgeSaveOmissions(ctx context.Context, k *types.Knowledge) []string {
	if !types.IsKnowledgeUserMutation(ctx) {
		return omitFieldsOnUpdate
	}
	id, name := r.knowledgeActor(ctx)
	if id == "" {
		return omitFieldsOnUpdate
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	k.LastModifiedBy, k.LastModifiedByName, k.LastModifiedAt = id, name, &now
	return []string{"DeletedAt", "PendingSubtasksCount", "CreatedBy", "CreatedByName"}
}

// mutationColumns is called only by explicit user-visible write paths. The
// default full-row Save omits these columns even if a stale worker struct has
// values, preventing lost attribution during asynchronous parsing/indexing.
func (r *knowledgeRepository) mutationColumns(ctx context.Context, values map[string]interface{}) map[string]interface{} {
	if values == nil {
		values = make(map[string]interface{})
	}
	for _, field := range knowledgeProvenanceColumns {
		delete(values, field)
	}
	if !types.IsKnowledgeUserMutation(ctx) {
		return values
	}
	id, name := r.knowledgeActor(ctx)
	if id != "" {
		values["last_modified_by"] = id
		values["last_modified_by_name"] = name
		values["last_modified_at"] = time.Now().UTC().Truncate(time.Microsecond)
	}
	return values
}
