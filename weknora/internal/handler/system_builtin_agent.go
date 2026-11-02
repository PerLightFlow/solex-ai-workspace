package handler

import (
	stderrors "errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

func (h *CustomAgentHandler) systemBuiltinService(c *gin.Context) interfaces.SystemBuiltinAgentService {
	ctx := c.Request.Context()
	_, key := types.TenantAPIKeyScopeFromContext(ctx)
	if !types.IsSystemAdminFromContext(ctx) || key {
		_ = c.Error(apperrors.NewForbiddenError("只有系统管理员可以管理全局内置智能体"))
		return nil
	}
	svc, ok := h.service.(interfaces.SystemBuiltinAgentService)
	if !ok {
		_ = c.Error(apperrors.NewInternalServerError("系统智能体目录不可用"))
		return nil
	}
	return svc
}

func systemBuiltinError(c *gin.Context, err error) {
	var appErr *apperrors.AppError
	switch {
	case stderrors.Is(err, service.ErrAgentNotFound):
		_ = c.Error(apperrors.NewNotFoundError("智能体已被移除或不可用"))
	case stderrors.Is(err, service.ErrAgentNameRequired):
		_ = c.Error(apperrors.NewBadRequestError(err.Error()))
	case stderrors.As(err, &appErr):
		_ = c.Error(appErr)
	default:
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		_ = c.Error(apperrors.NewInternalServerError("无法更新系统智能体目录"))
	}
}

func (h *CustomAgentHandler) ListSystemBuiltinAgents(c *gin.Context) {
	svc := h.systemBuiltinService(c)
	if svc == nil {
		return
	}
	data, err := svc.ListSystemBuiltinAgents(c.Request.Context())
	if err != nil {
		systemBuiltinError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *CustomAgentHandler) GetSystemBuiltinAgent(c *gin.Context) {
	svc := h.systemBuiltinService(c)
	if svc == nil {
		return
	}
	data, err := svc.GetSystemBuiltinAgent(c.Request.Context(), c.Param("id"))
	if err != nil {
		systemBuiltinError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *CustomAgentHandler) CreateSystemBuiltinAgent(c *gin.Context) {
	svc := h.systemBuiltinService(c)
	if svc == nil {
		return
	}
	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("无效的智能体配置"))
		return
	}
	if err := normalizeAgentReasoningEffort(&req.Config); err != nil {
		_ = c.Error(err)
		return
	}
	if _, err := checkAgentAllowedTools(nil, &req.Config); err != nil {
		_ = c.Error(err)
		return
	}
	a, err := svc.CreateSystemBuiltinAgent(c.Request.Context(), &types.CustomAgent{
		Name: req.Name, Description: req.Description, Avatar: req.Avatar, Config: req.Config})
	if err != nil {
		systemBuiltinError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": a})
}

func (h *CustomAgentHandler) UpdateSystemBuiltinAgent(c *gin.Context) {
	svc := h.systemBuiltinService(c)
	if svc == nil {
		return
	}
	var req UpdateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperrors.NewBadRequestError("无效的智能体配置"))
		return
	}
	if err := normalizeAgentReasoningEffort(&req.Config); err != nil {
		_ = c.Error(err)
		return
	}
	existing, err := svc.GetSystemBuiltinAgent(c.Request.Context(), c.Param("id"))
	if err != nil {
		systemBuiltinError(c, err)
		return
	}
	if _, err := checkAgentAllowedTools(existing.Config.AllowedTools, &req.Config); err != nil {
		_ = c.Error(err)
		return
	}
	a, err := svc.UpdateSystemBuiltinAgent(c.Request.Context(), &types.CustomAgent{
		ID: c.Param("id"), Name: req.Name, Description: req.Description, Config: req.Config}, req.Avatar)
	if err != nil {
		systemBuiltinError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": a})
}

func (h *CustomAgentHandler) DeleteSystemBuiltinAgent(c *gin.Context) {
	svc := h.systemBuiltinService(c)
	if svc == nil {
		return
	}
	if err := svc.DeleteSystemBuiltinAgent(c.Request.Context(), c.Param("id")); err != nil {
		systemBuiltinError(c, err)
		return
	}
	// Do not delete sessions, messages, or user channels: old history remains readable.
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "智能体已从全局目录移除"})
}
