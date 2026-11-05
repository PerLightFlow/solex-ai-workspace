package handler

import (
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/spaces"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *UserSpaceHandler) registerHistory(g *gin.RouterGroup) {
	g.GET("/knowledge-bases/:id/permission", func(c *gin.Context) {
		ctx := c.Request.Context()
		kind, id, tenant, e := h.service.ResourceScope(ctx, "knowledge_base", c.Param("id"), 0)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		a, e := h.service.Check(ctx, kind, id, tenant)
		if e == nil && !a.Read {
			e = spaces.ErrForbidden
		}
		spaceResponse(c, a, e)
	})
	g.GET("/recycled-resources", func(c *gin.Context) {
		rows, e := h.service.RecycledResources(c.Request.Context())
		spaceResponse(c, rows, e)
	})
	g.POST("/recycled-resources/restore", func(c *gin.Context) {
		var q struct {
			Kind   string `json:"kind"`
			ID     string `json:"id"`
			Tenant uint64 `json:"tenant_id"`
		}
		if c.ShouldBindJSON(&q) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.RestoreResource(c.Request.Context(), q.Kind, q.ID, q.Tenant))
	})
	g.GET("/retention", func(c *gin.Context) { spaceResponse(c, h.service.Retention(c.Request.Context()), nil) })
	g.PUT("/retention", func(c *gin.Context) {
		var r spaces.Retention
		if c.ShouldBindJSON(&r) != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		spaceResponse(c, nil, h.service.SaveRetention(c.Request.Context(), r))
	})
	g.GET("/knowledge/:id/versions", func(c *gin.Context) {
		rows, revision, e := h.service.Versions(c.Request.Context(), c.Param("id"))
		spaceResponse(c, gin.H{"versions": rows, "revision": revision}, e)
	})
	g.GET("/knowledge/:id/versions/:version", func(c *gin.Context) {
		v, e := strconv.ParseUint(c.Param("version"), 10, 64)
		if e != nil {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		k, e := h.service.KnowledgeVersion(c.Request.Context(), c.Param("id"), v)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		// Content only: never expose physical paths, pipeline metadata or credentials.
		out := gin.H{"title": k.Title, "file_name": k.FileName, "custom_metadata": k.CustomMetadata}
		if k.IsManual() {
			if m, e := k.ManualMetadata(); e == nil {
				out["content"] = m.Content
			}
		}
		spaceResponse(c, out, nil)
	})
	g.POST("/knowledge/:id/restore", func(c *gin.Context) {
		var q struct {
			Version  uint64 `json:"version"`
			Expected string `json:"expected_revision"`
		}
		if c.ShouldBindJSON(&q) != nil || q.Expected == "" {
			spaceResponse(c, nil, spaces.ErrInvalid)
			return
		}
		ctx := c.Request.Context()
		k, e := h.service.KnowledgeVersion(ctx, c.Param("id"), q.Version)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		if e = h.service.Require(ctx, "knowledge_base", k.KnowledgeBaseID, k.TenantID, "edit"); e != nil {
			spaceResponse(c, nil, e)
			return
		}
		kb, e := h.kbService.GetKnowledgeBaseByIDOnly(ctx, k.KnowledgeBaseID)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		ctx, e = access.WithKBTaskWrite(ctx, kb, k.TenantID)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		ctx = types.WithExpectedContentVersion(ctx, q.Expected)
		if e = h.service.RestoreSource(ctx, k); e != nil {
			spaceResponse(c, nil, e)
			return
		}
		ctx = types.WithExpectedContentVersion(ctx, "")
		_, e = h.knowledge.ReparseKnowledge(ctx, k.ID, nil)
		if e != nil {
			c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"restored": true, "indexing": "failed", "message": "原件已恢复；索引重建未启动，请在文档列表重试解析。"}})
			return
		}
		spaceResponse(c, gin.H{"restored": true, "indexing": "queued"}, nil)
	})
	g.GET("/knowledge-bases/:id/recycle-bin", func(c *gin.Context) {
		rows, e := h.service.RecycleBin(c.Request.Context(), c.Param("id"))
		spaceResponse(c, rows, e)
	})
	g.POST("/recycle/:id/restore", func(c *gin.Context) {
		ctx := c.Request.Context()
		if c.Query("kind") == "wiki" {
			p, e := h.service.ReviveWiki(ctx, c.Param("id"))
			if e != nil {
				spaceResponse(c, nil, e)
				return
			}
			ctx = types.WithWikiEditSource(types.WithContentRestore(types.WithExecutionTenant(ctx, p.TenantID)), types.WikiEditSourceRevert)
			_, e = h.wiki.UpdatePage(ctx, p)
			spaceResponse(c, gin.H{"restored": e == nil, "indexing": "queued"}, e)
			return
		}
		k, e := h.service.ReviveKnowledge(ctx, c.Param("id"))
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		kb, e := h.kbService.GetKnowledgeBaseByIDOnly(ctx, k.KnowledgeBaseID)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		ctx, e = access.WithKBTaskWrite(ctx, kb, k.TenantID)
		if e != nil {
			spaceResponse(c, nil, e)
			return
		}
		_, e = h.knowledge.ReparseKnowledge(ctx, k.ID, nil)
		if e != nil {
			c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"restored": true, "indexing": "failed", "message": "原件已恢复，请重试解析以重建索引。"}})
			return
		}
		spaceResponse(c, gin.H{"restored": true, "indexing": "queued"}, nil)
	})
}
