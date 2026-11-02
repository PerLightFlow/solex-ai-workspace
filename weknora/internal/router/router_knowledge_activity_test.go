package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type documentActivityServiceStub struct {
	interfaces.AuditLogService
	calls int
	list  func(uint64, string, string, uint64, int) ([]*types.KnowledgeActivity, error)
}

func (s *documentActivityServiceStub) ListKnowledgeActivity(
	_ context.Context, tenant uint64, kb, knowledge string, cursor uint64, limit int,
) ([]*types.KnowledgeActivity, error) {
	s.calls++
	return s.list(tenant, kb, knowledge, cursor, limit)
}

func newDocumentActivityRouter(t *testing.T, ownerTenant uint64, share interfaces.KBShareService, svc interfaces.AuditLogService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	enabled := true
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: ownerTenant, CreatorID: "another-user"}
	guards := &rbacGuards{
		cfg:              &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}},
		knowledgeService: &downloadKnowledgeLookup{knowledge: &types.Knowledge{ID: "doc-1", KnowledgeBaseID: kb.ID, TenantID: ownerTenant}},
		kbService:        &stubWikiKBLookup{kbs: map[string]*types.KnowledgeBase{kb.ID: kb}},
		kbShareService:   share,
	}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		ctx := types.WithCaller(c.Request.Context(), types.Caller{TenantID: 1, UserID: "reader", Role: types.TenantRoleViewer})
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Next()
	})
	RegisterKnowledgeBaseActivityRoutes(r.Group("/api/v1"), handler.NewAuditLogHandler(svc), guards)
	return r
}

func TestKnowledgeActivityRouteAllowsDocumentViewersAndScopesOwnerTenant(t *testing.T) {
	for _, owner := range []uint64{1, 2} {
		t.Run(map[uint64]string{1: "own workspace viewer", 2: "shared workspace viewer"}[owner], func(t *testing.T) {
			svc := &documentActivityServiceStub{list: func(tenant uint64, kb, knowledge string, cursor uint64, limit int) ([]*types.KnowledgeActivity, error) {
				require.Equal(t, owner, tenant)
				require.Equal(t, "kb-1", kb)
				require.Equal(t, "doc-1", knowledge)
				require.Equal(t, uint64(40), cursor)
				require.Equal(t, 1, limit)
				return []*types.KnowledgeActivity{{ID: 30, Action: types.AuditActionKnowledgeUpdated, ActorName: "Colleague", ActorKind: "user", ChangedFields: []string{"title"}}}, nil
			}}
			r := newDocumentActivityRouter(t, owner, &downloadKBShareStub{permission: types.OrgRoleViewer, source: owner}, svc)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/doc-1/activity?after_id=40&limit=1&actor=secret&outcome=denied&action=system.setting_changed", nil))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, svc.calls)
			var body struct {
				Data       []types.KnowledgeActivity `json:"data"`
				NextCursor uint64                    `json:"next_cursor"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, uint64(30), body.NextCursor)
			require.Len(t, body.Data, 1)
		})
	}
}

func TestKnowledgeActivityRouteRejectsUnauthorizedAndMissingDocument(t *testing.T) {
	svc := &documentActivityServiceStub{}
	r := newDocumentActivityRouter(t, 2, nil, svc)
	for _, document := range []string{"doc-1", "unrelated-document"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/"+document+"/activity", nil))
		require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, w.Code, w.Body.String())
	}
	require.Zero(t, svc.calls)
}
