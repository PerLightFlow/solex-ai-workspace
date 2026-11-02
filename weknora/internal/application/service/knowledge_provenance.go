package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func recordKnowledgeUpdated(ctx context.Context, audit interfaces.AuditLogService, knowledge *types.Knowledge, fields ...string) {
	if knowledge == nil || len(fields) == 0 {
		return
	}
	recordKBActivity(ctx, audit, knowledge.TenantID, knowledge.KnowledgeBaseID,
		types.AuditActionKnowledgeUpdated, "knowledge", knowledge.ID,
		types.AuditOutcomeSuccess, map[string]any{"changed_fields": fields})
}
