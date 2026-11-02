package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type knowledgeActivityStub struct {
	interfaces.AuditLogService
	list func(uint64, string, string, uint64, int) ([]*types.KnowledgeActivity, error)
}

func (s *knowledgeActivityStub) ListKnowledgeActivity(
	_ context.Context, tenant uint64, kb, knowledge string, cursor uint64, limit int,
) ([]*types.KnowledgeActivity, error) {
	return s.list(tenant, kb, knowledge, cursor, limit)
}

func TestKnowledgeActivityHandlerRequiresResolvedDocumentAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/knowledge/:id/activity", NewAuditLogHandler(&knowledgeActivityStub{list: func(uint64, string, string, uint64, int) ([]*types.KnowledgeActivity, error) {
		t.Fatal("must not query history without a read grant")
		return nil, nil
	}}).ListKnowledgeActivity)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge/doc-1/activity", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestKnowledgeActivityHandlerBoundsPageAndHidesServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fail := range []bool{false, true} {
		r := gin.New()
		r.Use(middleware.ErrorHandler())
		r.Use(func(c *gin.Context) {
			c.Set(middleware.KBAccessContextKey, &middleware.KBAccess{KnowledgeBase: &types.KnowledgeBase{ID: "kb-1", TenantID: 7}})
			c.Next()
		})
		r.GET("/knowledge/:id/activity", NewAuditLogHandler(&knowledgeActivityStub{list: func(tenant uint64, kb, knowledge string, cursor uint64, limit int) ([]*types.KnowledgeActivity, error) {
			require.Equal(t, uint64(7), tenant)
			require.Equal(t, "kb-1", kb)
			require.Equal(t, "doc-1", knowledge)
			require.Zero(t, cursor)
			require.Equal(t, 100, limit)
			if fail {
				return nil, errors.New("private query details")
			}
			return nil, nil
		}}).ListKnowledgeActivity)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge/doc-1/activity?after_id=garbage&limit=100000", nil))
		if fail {
			require.Equal(t, http.StatusInternalServerError, w.Code)
			require.NotContains(t, w.Body.String(), "private query details")
		} else {
			require.Equal(t, http.StatusOK, w.Code)
			require.JSONEq(t, `{"success":true,"data":[],"next_cursor":0}`, w.Body.String())
		}
	}
}
