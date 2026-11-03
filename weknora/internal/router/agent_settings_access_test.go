package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAgentRuntimeSettingsRejectWorkspaceOwnersEvenWithoutRBAC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enforced := range []bool{false, true} {
		g := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enforced}}}
		r := gin.New()
		r.Use(func(c *gin.Context) {
			ctx := context.WithValue(c.Request.Context(), types.TenantRoleContextKey, types.TenantRoleOwner)
			ctx = context.WithValue(ctx, types.UserIDContextKey, "ordinary-space-owner")
			c.Request = c.Request.WithContext(ctx)
		})
		RegisterSystemAdminRoutes(r.Group("/api/v1"), &handler.SystemHandler{}, nil, g)
		for _, key := range []string{"agent.max_iterations", "agent.collaboration_wait_timeout_seconds"} {
			for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(method, "/api/v1/system/admin/settings/"+key, strings.NewReader(`{"value":200}`))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				require.Equal(t, http.StatusForbidden, w.Code, "%s %s must stay system-admin-only (RBAC=%v)", method, key, enforced)
			}
		}
	}
}
