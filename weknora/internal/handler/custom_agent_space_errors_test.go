package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/spaces"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type rejectingAgentScope struct {
	interfaces.CustomAgentService
	err error
}

func (s rejectingAgentScope) GetAgentByID(context.Context, string) (*types.CustomAgent, error) {
	return &types.CustomAgent{ID: "test-agent", Name: "test"}, nil
}
func (s rejectingAgentScope) CreateAgent(context.Context, *types.CustomAgent) (*types.CustomAgent, error) {
	return nil, s.err
}
func (s rejectingAgentScope) UpdateAgent(context.Context, *types.CustomAgent, *string) (*types.CustomAgent, error) {
	return nil, s.err
}

func TestAgentWriteReturnsKnowledgeScopeDenial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		for _, tc := range []struct {
			err    error
			status int
		}{
			{fmt.Errorf("knowledge selection: %w", spaces.ErrForbidden), http.StatusForbidden},
			{fmt.Errorf("shared agent: %w", spaces.ErrInvalid), http.StatusBadRequest},
		} {
			r := gin.New()
			r.Use(middleware.ErrorHandler())
			h := &CustomAgentHandler{service: rejectingAgentScope{err: tc.err}}
			r.POST("/agents/:id", h.CreateAgent)
			r.PUT("/agents/:id", h.UpdateAgent)
			r.PATCH("/agents/:id", h.PatchAgent)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/agents/test-agent", strings.NewReader(`{"name":"test","config":{"kb_selection_mode":"selected","knowledge_bases":["unavailable"]}}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, "%s: %s", method, rec.Body.String())
		}
	}
}
