package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/spaces"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UserSpaceHandler struct {
	service   *spaces.Service
	knowledge interfaces.KnowledgeService
	kbService interfaces.KnowledgeBaseService
	wiki      interfaces.WikiPageService
}

func NewUserSpaceHandler(db *gorm.DB, knowledge interfaces.KnowledgeService, kb interfaces.KnowledgeBaseService, wiki interfaces.WikiPageService) *UserSpaceHandler {
	s := spaces.New(db)
	spaces.Install(s)
	return &UserSpaceHandler{service: s, knowledge: knowledge, kbService: kb, wiki: wiki}
}

func spaceResponse(c *gin.Context, value any, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": value})
		return
	}
	status := http.StatusInternalServerError
	message := "共享空间操作失败"
	switch {
	case errors.Is(err, spaces.ErrForbidden):
		status = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, spaces.ErrNotFound):
		status = http.StatusNotFound
		message = err.Error()
	case errors.Is(err, spaces.ErrInvalid):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, spaces.ErrConflict):
		status = http.StatusConflict
		message = err.Error()
	}
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message})
}

// Middleware closes every legacy membership/grant write entrance. It also
// prevents selecting another personal tenant, even with a cached old JWT.
func (h *UserSpaceHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !types.UserSpacesEnabled() {
			c.Next()
			return
		}
		if types.CallerFromContext(c.Request.Context()).UserID != "" {
			if e := h.service.ValidateActor(c.Request.Context()); e != nil {
				spaceResponse(c, nil, e)
				return
			}
		}
		if !h.service.Ready(c.Request.Context()) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"message": "共享空间迁移尚未完成"})
			return
		}
		path := c.FullPath()
		method := c.Request.Method
		if method == "POST" && (path == "/api/v1/agents" || path == "/api/v1/knowledge-bases") {
			if target := c.GetHeader("X-Solex-Space-ID"); target != "" {
				c.Request = c.Request.WithContext(types.WithCreatedResourceSpace(c.Request.Context(), target))
			}
		}
		if method == "GET" && path == "/api/v1/organizations" {
			rows, e := h.service.List(c.Request.Context())
			spaceResponse(c, rows, e)
			c.Abort()
			return
		}
		write := method != "GET" && method != "HEAD" && method != "OPTIONS"
		legacy := strings.HasPrefix(path, "/api/v1/organizations") || strings.HasSuffix(path, "/shares") || strings.Contains(path, "/shares/")
		personalMembers := strings.HasPrefix(path, "/api/v1/tenants") && (strings.Contains(path, "/members") || strings.Contains(path, "/invitations") || strings.Contains(path, "/invite-links") || strings.HasSuffix(path, "/leave"))
		personalCreation := path == "/api/v1/tenants" || path == "/api/v1/auth/register-by-invite" || strings.HasPrefix(path, "/api/v1/me/invitations")
		if legacy || personalMembers || (write && personalCreation) || (method == "DELETE" && strings.HasPrefix(path, "/api/v1/tenants/")) {
			spaceResponse(c, nil, spaces.ErrForbidden)
			return
		}
		if path == "/api/v1/auth/switch-tenant" {
			spaceResponse(c, nil, spaces.ErrForbidden)
			return
		}
		if e := h.resourceGuard(c); e != nil {
			spaceResponse(c, nil, e)
			return
		}
		c.Next()
	}
}

func (h *UserSpaceHandler) Register(r *gin.RouterGroup) {
	g := r.Group("/spaces", func(c *gin.Context) {
		if !types.UserSpacesEnabled() {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	})
	h.registerHistory(g)
	h.registerLifecycle(g)
	g.GET("/resources/:kind/:id", func(c *gin.Context) {
		ctx := c.Request.Context()
		tenant := types.CallerFromContext(ctx).TenantID
		if value := c.Query("tenant"); value != "" {
			t, e := strconv.ParseUint(value, 10, 64)
			if e != nil {
				spaceResponse(c, nil, spaces.ErrInvalid)
				return
			}
			tenant = t
		}
		kind, id, source, e := h.service.ResourceScope(ctx, c.Param("kind"), c.Param("id"), tenant)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		v, e := h.service.InspectResource(ctx, kind, id, source)
		spaceResponse(c, v, e)
	})
	g.GET("", func(c *gin.Context) { v, e := h.service.List(c.Request.Context()); spaceResponse(c, v, e) })
	g.POST("", func(c *gin.Context) {
		var q struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		v, e := h.service.Create(c.Request.Context(), q.Name, q.Description)
		spaceResponse(c, v, e)
	})
	g.GET("/invitations", func(c *gin.Context) { v, e := h.service.Invitations(c.Request.Context()); spaceResponse(c, v, e) })
	g.POST("/invitations/:invitation/accept", func(c *gin.Context) {
		spaceResponse(c, nil, h.service.Accept(c.Request.Context(), c.Param("invitation")))
	})
	g.GET("/:space/members", func(c *gin.Context) {
		v, e := h.service.Members(c.Request.Context(), c.Param("space"))
		spaceResponse(c, v, e)
	})
	g.POST("/:space/invitations", func(c *gin.Context) {
		var q struct {
			Email  string `json:"email"`
			UserID string `json:"user_id"`
		}
		if c.ShouldBindJSON(&q) != nil || (q.Email != "" && q.UserID != "") {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		if q.UserID != "" {
			v, e := h.service.InviteUser(c.Request.Context(), c.Param("space"), q.UserID)
			spaceResponse(c, v, e)
			return
		}
		v, e := h.service.Invite(c.Request.Context(), c.Param("space"), q.Email)
		spaceResponse(c, v, e)
	})
	g.GET("/:space/invite-candidates", func(c *gin.Context) {
		offset, offsetErr := strconv.Atoi(c.DefaultQuery("offset", "0"))
		limit, limitErr := strconv.Atoi(c.DefaultQuery("limit", "20"))
		if offsetErr != nil || limitErr != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		v, e := h.service.InviteCandidates(c.Request.Context(), c.Param("space"), c.Query("q"), offset, limit)
		spaceResponse(c, v, e)
	})
	g.PUT("/:space/members/:user", func(c *gin.Context) {
		var q struct {
			Role string `json:"role"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.SetMemberRole(c.Request.Context(), c.Param("space"), c.Param("user"), q.Role))
	})
	g.DELETE("/:space/members/:user", func(c *gin.Context) {
		spaceResponse(c, nil, h.service.SetMemberRole(c.Request.Context(), c.Param("space"), c.Param("user"), ""))
	})
	g.POST("/:space/transfer", func(c *gin.Context) {
		var q struct {
			UserID string `json:"user_id"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.TransferSpace(c.Request.Context(), c.Param("space"), q.UserID))
	})
	g.DELETE("/:space", func(c *gin.Context) { spaceResponse(c, nil, h.service.Delete(c.Request.Context(), c.Param("space"))) })
	g.GET("/:space/resources", func(c *gin.Context) {
		v, e := h.service.Resources(c.Request.Context(), c.Param("space"))
		spaceResponse(c, v, e)
	})
	g.GET("/:space/events", func(c *gin.Context) {
		v, e := h.service.Events(c.Request.Context(), c.Param("space"))
		spaceResponse(c, v, e)
	})
	type resourceRequest struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Tenant   uint64 `json:"source_tenant_id"`
		Edit     bool   `json:"allow_edit"`
		Download bool   `json:"allow_download"`
	}
	g.POST("/:space/resources", func(c *gin.Context) {
		var q resourceRequest
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.Share(c.Request.Context(), c.Param("space"), q.Kind, q.ID, q.Tenant, q.Edit, q.Download))
	})
	g.DELETE("/:space/resources", func(c *gin.Context) {
		var q resourceRequest
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.Unshare(c.Request.Context(), c.Param("space"), q.Kind, q.ID, q.Tenant))
	})
	g.POST("/:space/resources/transfer", func(c *gin.Context) {
		var q resourceRequest
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.Transfer(c.Request.Context(), q.Kind, q.ID, q.Tenant, c.Param("space")))
	})
}

// resourceGuard is an additional boundary over every resource route, including
// old handlers which predate user-level sharing. Storage scope cannot bypass it.
func (h *UserSpaceHandler) resourceGuard(c *gin.Context) error {
	ctx := c.Request.Context()
	path := c.FullPath()
	method := c.Request.Method
	kind, id := "", ""
	tenant := types.CallerFromContext(ctx).TenantID
	switch {
	case strings.HasPrefix(path, "/api/v1/knowledge-bases/:id"):
		kind, id = "knowledge_base", c.Param("id")
	case strings.HasPrefix(path, "/api/v1/knowledge/:id") || strings.HasPrefix(path, "/api/v1/knowledge/manual/:id") || strings.HasPrefix(path, "/api/v1/knowledge/image/:id"):
		kind, id = "knowledge", c.Param("id")
	case strings.HasPrefix(path, "/api/v1/chunks/by-id/:id"):
		kind, id = "chunk", c.Param("id")
	case strings.HasPrefix(path, "/api/v1/chunks/:knowledge_id"):
		kind, id = "knowledge", c.Param("knowledge_id")
	case strings.Contains(path, "/wiki/") && c.Param("kb_id") != "":
		kind, id = "knowledge_base", c.Param("kb_id")
	case strings.HasPrefix(path, "/api/v1/agents/:id"):
		kind, id = "agent", c.Param("id")
		if types.IsBuiltinAgentID(id) {
			return nil
		}
		if value := c.GetHeader("X-Source-Tenant-ID"); value != "" {
			t, e := strconv.ParseUint(value, 10, 64)
			if e != nil {
				return spaces.ErrInvalid
			}
			tenant = t
		}
	}
	if id == "" {
		return nil
	}
	resolvedKind, resource, source, e := h.service.ResourceScope(ctx, kind, id, tenant)
	if e != nil {
		return e
	}
	permission := "read"
	if kind == "agent" {
		permission = "edit"
	}
	if method != "GET" && method != "HEAD" && method != "OPTIONS" {
		permission = "edit"
	}
	for _, suffix := range []string{"/search", "/hybrid-search", "/pin"} {
		if strings.HasSuffix(path, suffix) {
			permission = "read"
		}
	}
	if strings.Contains(path, "download") || strings.HasSuffix(path, "/export") || strings.HasSuffix(path, "/preview") {
		permission = "download"
	}
	if (kind == "knowledge_base" && path == "/api/v1/knowledge-bases/:id" && method != "GET") || (kind == "agent" && method == "DELETE") || strings.Contains(path, "/copy") || strings.Contains(path, "/duplicate") {
		permission = "manage"
	}
	if e = h.service.Require(ctx, resolvedKind, resource, source, permission); e != nil {
		return e
	}
	if method == "DELETE" && (path == "/api/v1/knowledge-bases/:id" || path == "/api/v1/agents/:id") {
		if e = h.service.RecycleResource(ctx, kind, id, source); e != nil {
			return e
		}
		spaceResponse(c, nil, nil)
		c.Abort()
		return nil
	}
	if kind == "knowledge" && method == "GET" {
		if _, revision, e := h.service.Versions(ctx, id); e == nil {
			c.Header("X-Solex-Revision", revision)
		}
	}
	if kind == "knowledge" && (method == "PUT" || method == "PATCH" || strings.HasSuffix(path, "/replace")) {
		if revision := c.GetHeader("X-Solex-Revision"); revision != "" {
			ctx = types.WithExpectedContentVersion(ctx, revision)
		}
	}
	if kind == "agent" {
		ctx = types.WithExecutionTenant(ctx, source)
	}
	c.Request = c.Request.WithContext(ctx)
	return nil
}
