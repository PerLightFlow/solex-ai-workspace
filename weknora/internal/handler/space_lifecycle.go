package handler

import (
	"github.com/Tencent/WeKnora/internal/spaces"
	"github.com/gin-gonic/gin"
)

func (h *UserSpaceHandler) registerLifecycle(g *gin.RouterGroup) {
	g.GET("/admin/users/:user/handover", func(c *gin.Context) {
		v, e := h.service.UserHandover(c.Request.Context(), c.Param("user"))
		spaceResponse(c, v, e)
	})
	g.GET("/admin/resource-handovers", func(c *gin.Context) {
		v, e := h.service.OrphanedSharedResources(c.Request.Context())
		spaceResponse(c, v, e)
	})
	g.POST("/admin/resource-handovers", func(c *gin.Context) {
		var q struct {
			Kind   string `json:"kind"`
			ID     string `json:"resource_id"`
			Tenant uint64 `json:"source_tenant_id"`
			Space  string `json:"space_id"`
			Owner  string `json:"owner_user_id"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.AdminResourceHandover(c.Request.Context(), q.Kind, q.ID, q.Tenant, q.Space, q.Owner))
	})
	g.PUT("/:space/archive", func(c *gin.Context) {
		var q struct {
			Archived bool `json:"archived"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.Archive(c.Request.Context(), c.Param("space"), q.Archived))
	})
	g.GET("/admin/handovers", func(c *gin.Context) { v, e := h.service.OrphanedSpaces(c.Request.Context()); spaceResponse(c, v, e) })
	g.POST("/admin/handovers/:space", func(c *gin.Context) {
		var q struct {
			UserID string `json:"user_id"`
			Owner  string `json:"owner_user_id"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.AdminHandover(c.Request.Context(), c.Param("space"), q.UserID, q.Owner))
	})
	g.GET("/:space/invitations", func(c *gin.Context) {
		v, e := h.service.PendingInvitations(c.Request.Context(), c.Param("space"))
		spaceResponse(c, v, e)
	})
	g.DELETE("/invitations/:invitation", func(c *gin.Context) {
		spaceResponse(c, nil, h.service.CancelInvitation(c.Request.Context(), c.Param("invitation")))
	})
}
