package session

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Attachment validation happens before the session runtime builds its model
// config. Resolve optional shared models here too, without mutating the stored
// agent or changing which tenant owns the conversation and uploaded files.
func (h *Handler) withManagedAttachmentModels(ctx context.Context, agent *types.CustomAgent) *types.CustomAgent {
	if agent == nil || !types.ManagedRuntimeEnabled() {
		return agent
	}
	// Global definitions inherit runtime models for administrators too. Their
	// personal agents keep explicit model choices and parser settings.
	if types.IsSystemAdminFromContext(ctx) && !(agent.IsBuiltin && types.IsSystemBuiltinAgentID(agent.ID)) {
		return agent
	}
	copy := *agent
	copy.Config.ModelID = ""
	copy.Config.ASRModelID = ""
	copy.Config.VLMModelID = ""
	copy.Config.ChatParserEngineRules = nil
	copy.Config.ImageStorageProvider = ""
	if h.modelService == nil {
		return &copy
	}
	models, err := h.modelService.ListModels(ctx)
	if err != nil {
		return &copy
	}
	if model := types.PickManagedDefaultModel(models, types.ModelTypeKnowledgeQA); model != nil {
		copy.Config.ModelID = model.ID
	}
	if model := types.PickManagedDefaultModel(models, types.ModelTypeVLLM); model != nil {
		copy.Config.VLMModelID = model.ID
	}
	if model := types.PickManagedDefaultModel(models, types.ModelTypeASR); model != nil {
		copy.Config.ASRModelID = model.ID
	}
	return &copy
}
