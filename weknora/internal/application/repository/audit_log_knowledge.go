package repository

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
)

// ListKnowledgeActivity queries the authorized document before pagination. It
// never scans a workspace feed and filters afterwards, which could both leak
// unrelated events and leave holes in cursor pagination.
func (r *auditLogRepository) ListKnowledgeActivity(
	ctx context.Context, tenantID uint64, kbID, knowledgeID string, afterID uint64, limit int,
) ([]*types.KnowledgeActivity, error) {
	result := make([]*types.KnowledgeActivity, 0)
	if tenantID == 0 || kbID == "" || knowledgeID == "" {
		return result, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > auditLogListLimitMax {
		limit = auditLogListLimitMax
	}
	query := r.db.WithContext(ctx).Where("tenant_id = ? AND scope_type = ? AND scope_id = ?",
		tenantID, "knowledge_base", kbID).
		Where("(target_type = ? AND target_id = ?) OR (target_type = ? AND details ->> 'knowledge_id' = ?)",
			"knowledge", knowledgeID, "chunk", knowledgeID).
		Where("action IN ?", []types.AuditAction{
			types.AuditActionKnowledgeCreated, types.AuditActionKnowledgeUpdated,
			types.AuditActionKnowledgeDeleted, types.AuditActionKnowledgeReparseStarted,
			types.AuditActionKnowledgeParseCanceled, types.AuditActionKnowledgeMoveCompleted,
		}).
		Where("outcome = ? OR (outcome = ? AND action IN ?)",
			types.AuditOutcomeSuccess, types.AuditOutcomeAccepted, []types.AuditAction{
				types.AuditActionKnowledgeCreated, types.AuditActionKnowledgeUpdated,
				types.AuditActionKnowledgeReparseStarted,
			})
	if afterID > 0 {
		query = query.Where("id < ?", afterID)
	}
	var entries []*types.AuditLog
	if err := query.Order("id DESC").Limit(limit).Find(&entries).Error; err != nil {
		return nil, err
	}

	// Resolve only the actors in this authorized page. A shared contributor's
	// account may belong to another tenant, so user lookup is by immutable user
	// ID; no user directory or email is exposed to document readers.
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.ActorUserID != "" && !types.IsSyntheticUserID(entry.ActorUserID) {
			ids = append(ids, entry.ActorUserID)
		}
	}
	names := make(map[string]string, len(ids))
	if len(ids) > 0 {
		var users []types.User
		if err := r.db.WithContext(ctx).Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			names[user.ID] = safeActivityActorName(user.Username)
		}
	}
	for _, entry := range entries {
		result = append(result, projectKnowledgeActivity(entry, names))
	}
	return result, nil
}

type knowledgeActivityDetails struct {
	ActorName     string   `json:"actor_name"`
	ActorKind     string   `json:"actor_kind"`
	Trigger       string   `json:"trigger"`
	ChangedFields []string `json:"changed_fields"`
}

func projectKnowledgeActivity(entry *types.AuditLog, names map[string]string) *types.KnowledgeActivity {
	var details knowledgeActivityDetails
	_ = json.Unmarshal(entry.Details, &details)
	item := &types.KnowledgeActivity{
		ID: entry.ID, Action: entry.Action, CreatedAt: entry.CreatedAt,
		ActorKind: "unknown", ChangedFields: make([]string, 0),
	}
	if entry.ActorUserID != "" && !types.IsSyntheticUserID(entry.ActorUserID) {
		// The snapshot preserves attribution when a user is renamed or deleted.
		// Old events without a snapshot use the current account name.
		item.ActorName = safeActivityActorName(details.ActorName)
		if item.ActorName == "" {
			item.ActorName = names[entry.ActorUserID]
		}
		if item.ActorName != "" {
			item.ActorKind = "user"
		}
	} else if details.ActorKind == "system" || types.IsSyntheticUserID(entry.ActorUserID) || details.Trigger == "system" {
		item.ActorKind = "system"
	}
	seen := make(map[string]bool)
	for _, field := range details.ChangedFields {
		// Custom metadata keys and before/after values may contain sensitive
		// project content. Only these stable field categories are public.
		switch field {
		case "title", "description", "custom_metadata", "content", "file", "tags", "folder_path", "knowledge_base_id", "chunks":
			if !seen[field] {
				item.ChangedFields = append(item.ChangedFields, field)
				seen[field] = true
			}
		}
	}
	return item
}

func safeActivityActorName(name string) string {
	name = strings.TrimSpace(name)
	// Never fall back to an email address as a document's public attribution.
	if strings.Contains(name, "@") || len([]rune(name)) > 100 {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
}
