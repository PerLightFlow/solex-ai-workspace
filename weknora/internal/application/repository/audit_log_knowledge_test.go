package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeActivityScopePaginationAndProjection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.AuditLog{}))
	require.NoError(t, db.Exec("CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO users (id, username) VALUES ('alice-id', 'Alice'), ('email-id', 'private@example.test')").Error)
	makeEntry := func(id uint64, change func(*types.AuditLog)) *types.AuditLog {
		entry := &types.AuditLog{
			ID: id, TenantID: 7, ScopeType: "knowledge_base", ScopeID: "kb-1", TargetType: "knowledge", TargetID: "doc-1",
			Action: types.AuditActionKnowledgeUpdated, Outcome: types.AuditOutcomeSuccess, ActorUserID: "alice-id",
			RequestPath: "/private/request/path", RequestMethod: "PUT", CreatedAt: time.Now(),
			Details: types.JSON(`{"changed_fields":["title","api_key","secret-project-key","title"],"before":"private body","api_key":"sk-secret"}`),
		}
		if change != nil {
			change(entry)
		}
		return entry
	}
	rows := []*types.AuditLog{
		makeEntry(10, nil),
		makeEntry(11, func(e *types.AuditLog) { e.TargetID = "other-document" }),
		makeEntry(12, func(e *types.AuditLog) { e.ScopeID = "other-kb" }),
		makeEntry(13, func(e *types.AuditLog) { e.TenantID = 8 }),
		makeEntry(14, func(e *types.AuditLog) { e.Outcome = types.AuditOutcomeDenied }),
		makeEntry(15, func(e *types.AuditLog) { e.Action = types.AuditActionSystemSettingChanged }),
		makeEntry(16, func(e *types.AuditLog) {
			e.Action = types.AuditActionKnowledgeCreated
			e.Outcome = types.AuditOutcomeAccepted
		}),
		makeEntry(17, func(e *types.AuditLog) {
			e.TargetType, e.TargetID = "chunk", "chunk-1"
			e.Details = types.JSON(`{"knowledge_id":"doc-1","changed_fields":["chunks"]}`)
		}),
		makeEntry(18, func(e *types.AuditLog) {
			e.TargetType, e.TargetID = "chunk", "chunk-other"
			e.Details = types.JSON(`{"knowledge_id":"other-document","changed_fields":["chunks"]}`)
		}),
		makeEntry(19, func(e *types.AuditLog) { e.ActorUserID = "email-id" }),
		makeEntry(20, func(e *types.AuditLog) { e.ActorUserID = "missing-id" }),
		makeEntry(21, func(e *types.AuditLog) {
			e.ActorUserID = ""
			e.Details = types.JSON(`{"trigger":"system","actor_kind":"system"}`)
		}),
		makeEntry(22, func(e *types.AuditLog) {
			e.ActorUserID = "deleted-id"
			e.Details = types.JSON(`{"actor_name":"Former colleague","actor_kind":"user","changed_fields":["content"]}`)
		}),
		makeEntry(23, func(e *types.AuditLog) { e.Outcome = types.AuditOutcomeFailed }),
		makeEntry(24, func(e *types.AuditLog) { e.TargetType, e.TargetID = "chunk", "doc-1" }),
		makeEntry(25, func(e *types.AuditLog) { e.ScopeType = "" }),
		makeEntry(26, func(e *types.AuditLog) { e.Outcome = types.AuditOutcomeAccepted }),
		makeEntry(27, func(e *types.AuditLog) {
			e.Outcome = types.AuditOutcomeAccepted
			e.Action = types.AuditActionKnowledgeDeleted
		}),
	}
	require.NoError(t, db.Create(&rows).Error)
	repo := NewAuditLogRepository(db)
	var ids []uint64
	var cursor uint64
	var all []*types.KnowledgeActivity
	for {
		page, err := repo.ListKnowledgeActivity(context.Background(), 7, "kb-1", "doc-1", cursor, 2)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.LessOrEqual(t, len(page), 2)
		for _, item := range page {
			ids = append(ids, item.ID)
		}
		all = append(all, page...)
		cursor = page[len(page)-1].ID
	}
	require.Equal(t, []uint64{26, 22, 21, 20, 19, 17, 16, 10}, ids)
	require.Equal(t, "Alice", all[0].ActorName)
	require.Equal(t, []string{"title"}, all[0].ChangedFields)
	require.Equal(t, "Former colleague", all[1].ActorName)
	require.Equal(t, "system", all[2].ActorKind)
	require.Equal(t, "unknown", all[3].ActorKind)
	require.Equal(t, "unknown", all[4].ActorKind)
	require.Empty(t, all[4].ActorName)
	require.Equal(t, []string{"chunks"}, all[5].ChangedFields)
	encoded, err := json.Marshal(all)
	require.NoError(t, err)
	for _, forbidden := range []string{"alice-id", "private@example", "request_path", "private body", "sk-secret", "api_key", "secret-project-key", "tenant_id", "details"} {
		require.False(t, strings.Contains(string(encoded), forbidden), "leaked %s: %s", forbidden, encoded)
	}
	missing, err := repo.ListKnowledgeActivity(context.Background(), 7, "kb-1", "unknown-doc", 0, 50)
	require.NoError(t, err)
	require.Empty(t, missing)
	invalid, err := repo.ListKnowledgeActivity(context.Background(), 0, "", "doc-1", 0, 50)
	require.NoError(t, err)
	require.Empty(t, invalid)
}

func TestKnowledgeActivityProjectionDoesNotInventIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, actor string
		details     string
		kind        string
	}{
		{"legacy anonymous", "", `{}`, "unknown"},
		{"machine identity", "system-7", `{}`, "system"},
		{"deleted account without snapshot", "gone", `{}`, "unknown"},
		{"email snapshot", "gone", `{"actor_name":"private@example.test"}`, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := projectKnowledgeActivity(&types.AuditLog{ActorUserID: tc.actor, Details: types.JSON(tc.details)}, nil)
			require.Equal(t, tc.kind, item.ActorKind)
			require.Empty(t, item.ActorName)
			require.NotNil(t, item.ChangedFields)
		})
	}
}
