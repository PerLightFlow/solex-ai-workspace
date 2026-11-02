package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeMetadataAuditOnlyRecordsCommittedChanges(t *testing.T) {
	f := newDocumentWriteFixture(t)
	audit := &captureKBActivityAudit{}
	f.svc.audit = audit
	ctx := context.WithValue(f.ctx, types.UserIDContextKey, "editor")
	ctx = context.WithValue(ctx, types.UserContextKey, &types.User{ID: "editor", Username: "Editor"})
	// A no-op should neither stamp a new editor nor emit an edit event.
	require.NoError(t, f.svc.UpdateKnowledge(ctx, &types.Knowledge{ID: "doc", Title: "original", CustomMetadata: types.JSON(`{}`)}))
	require.Zero(t, f.repo.writes)
	require.Nil(t, audit.entry)
	require.NoError(t, f.svc.UpdateKnowledge(ctx, &types.Knowledge{ID: "doc", Title: "Changed", Description: "Summary", CustomMetadata: types.JSON(`{"chip":"test"}`)}))
	require.Equal(t, 1, f.repo.writes)
	require.NotNil(t, audit.entry)
	require.Equal(t, types.AuditActionKnowledgeUpdated, audit.entry.Action)
	require.Equal(t, "doc", audit.entry.TargetID)
	var details struct {
		ActorName     string   `json:"actor_name"`
		ChangedFields []string `json:"changed_fields"`
	}
	require.NoError(t, json.Unmarshal(audit.entry.Details, &details))
	require.Equal(t, "Editor", details.ActorName)
	require.ElementsMatch(t, []string{"title", "description", "custom_metadata"}, details.ChangedFields)
	got, err := f.repo.GetKnowledgeByID(ctx, 7, "doc")
	require.NoError(t, err)
	require.Equal(t, "editor", got.LastModifiedBy)
	require.NotNil(t, got.LastModifiedAt)
}
