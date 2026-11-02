package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSystemBuiltinAgentHTTPCatalogAndAuthorization(t *testing.T) {
	t.Setenv("SOLEX_MANAGED_RUNTIME", "true")
	gin.SetMode(gin.TestMode)
	require.NoError(t, types.LoadBuiltinAgentsConfig(filepath.Join("..", "..", "config")))
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "agents.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.SystemBuiltinAgent{}, &types.CustomAgent{}))
	h := &CustomAgentHandler{service: service.NewCustomAgentService(repository.NewCustomAgentRepository(db), nil, nil, nil, nil, nil, nil, nil)}
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	g := r.Group("/system/builtin-agents", middleware.RequireSystemAdmin(&config.Config{}))
	g.GET("", h.ListSystemBuiltinAgents)
	g.GET("/:id", h.GetSystemBuiltinAgent)
	g.POST("", h.CreateSystemBuiltinAgent)
	g.PUT("/:id", h.UpdateSystemBuiltinAgent)
	g.DELETE("/:id", h.DeleteSystemBuiltinAgent)
	r.GET("/agents/:id", h.GetAgent)
	r.DELETE("/agents/:id", h.DeleteAgent)
	member := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(22))
	member = context.WithValue(member, types.TenantRoleContextKey, types.TenantRoleOwner)
	admin := context.WithValue(member, types.SystemAdminContextKey, true)
	request := func(ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for _, ctx := range []context.Context{member, types.WithTenantAPIKeyScope(admin, types.TenantAPIKeyScope{ScopeType: types.APIKeyScopePlatform})} {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			path := "/system/builtin-agents"
			if method == "PUT" || method == "DELETE" {
				path += "/builtin-smart-reasoning"
			}
			rec := request(ctx, method, path, `{"name":"blocked"}`)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		}
	}
	bad := request(admin, "POST", "/system/builtin-agents", `{"name":"unsafe","config":{"model_id":"private-model"}}`)
	require.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())
	created := request(admin, "POST", "/system/builtin-agents", `{"name":"Shared","avatar":"S","id":"builtin-weknora-assistant","config":{"kb_selection_mode":"all"}}`)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var body struct {
		Data types.CustomAgent `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &body))
	id := body.Data.ID
	require.NotEqual(t, types.BuiltinWeKnoraAssistantID, id)
	read := request(member, "GET", "/agents/"+id, "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	// No IM service was supplied: forbidden legacy delete must return before any channel mutation.
	require.Equal(t, http.StatusForbidden, request(member, "DELETE", "/agents/"+id, "").Code)
	updated := request(admin, "PUT", "/system/builtin-agents/"+id, `{"name":"Renamed","config":{"agent_mode":"smart-reasoning","kb_selection_mode":"all"}}`)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &body))
	require.Equal(t, "S", body.Data.Avatar)
	require.Equal(t, "Renamed", body.Data.Name)
	require.Equal(t, http.StatusOK, request(admin, "DELETE", "/system/builtin-agents/"+id, "").Code)
	for i := 0; i < 2; i++ {
		require.Equal(t, http.StatusNotFound, request(member, "GET", "/agents/"+id, "").Code)
	}
}
