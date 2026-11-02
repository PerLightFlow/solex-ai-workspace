package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type knowledgeActivityResponse struct {
	Success    bool                       `json:"success"`
	Data       []*types.KnowledgeActivity `json:"data"`
	NextCursor uint64                     `json:"next_cursor"`
}

// ListKnowledgeActivity exposes only the safe document mutation projection.
// KBAccessReadFromKnowledgeIDParam must precede this handler: it resolves the
// requested document to its parent KB and grants read access, including shares.
// The separate KB/tenant audit feeds retain their stricter administrator guard.
func (h *AuditLogHandler) ListKnowledgeActivity(c *gin.Context) {
	grant, ok := middleware.KBAccessFromContext(c)
	if !ok || grant.KnowledgeBase == nil || grant.KnowledgeBase.TenantID == 0 || c.Param("id") == "" {
		c.Error(errors.NewNotFoundError("knowledge not found"))
		return
	}
	afterID, limit := parseAuditCursor(c)
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	ctx := c.Request.Context()
	entries, err := h.auditService.ListKnowledgeActivity(ctx, grant.KnowledgeBase.TenantID,
		grant.KnowledgeBase.ID, c.Param("id"), afterID, limit)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"knowledge_id": c.Param("id")})
		c.Error(errors.NewInternalServerError("cannot load document activity"))
		return
	}
	if entries == nil {
		entries = make([]*types.KnowledgeActivity, 0)
	}
	var nextCursor uint64
	if len(entries) == limit {
		nextCursor = entries[len(entries)-1].ID
	}
	c.JSON(http.StatusOK, knowledgeActivityResponse{
		Success: true, Data: entries, NextCursor: nextCursor,
	})
}
