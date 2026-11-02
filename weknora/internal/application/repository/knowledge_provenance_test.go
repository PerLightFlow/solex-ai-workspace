package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func provenanceContext(id, name string) context.Context {
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, id)
	return context.WithValue(ctx, types.UserContextKey, &types.User{ID: id, Username: name})
}

func provenanceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "provenance.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Chunk{}, &types.ChunkRevision{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestKnowledgeProvenancePreservesUploaderAndHumanEditAcrossWorkerSaves(t *testing.T) {
	db := provenanceDB(t)
	repo := NewKnowledgeRepository(db)
	alice, bob := provenanceContext("alice", "Alice"), provenanceContext("bob", "Bob")
	doc := &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb", Title: "Original", CreatedBy: "forged", CreatedByName: "forged"}
	require.NoError(t, repo.CreateKnowledge(alice, doc))
	require.Equal(t, "alice", doc.CreatedBy)
	require.Equal(t, "Alice", doc.CreatedByName)
	stale := *doc
	doc.Title = "Bob's title"
	doc.CreatedBy, doc.CreatedByName = "forged", "forged"
	require.NoError(t, repo.UpdateKnowledge(types.WithKnowledgeUserMutation(bob), doc))
	edited, err := repo.GetKnowledgeByID(context.Background(), 7, doc.ID)
	require.NoError(t, err)
	require.Equal(t, "alice", edited.CreatedBy)
	require.Equal(t, "bob", edited.LastModifiedBy)
	require.Equal(t, "Bob", edited.LastModifiedByName)
	require.NotNil(t, edited.LastModifiedAt)
	// A worker may retain the uploader identity and an old copy of the row.
	stale.Description = "Generated summary"
	require.NoError(t, repo.UpdateKnowledge(alice, &stale))
	require.NoError(t, repo.UpdateKnowledgeColumns(alice, doc.ID, map[string]interface{}{
		"parse_status": "completed", "created_by": "forged", "last_modified_by": "forged", "last_modified_at": time.Now().Add(time.Hour),
	}))
	got, err := repo.GetKnowledgeByID(context.Background(), 7, doc.ID)
	require.NoError(t, err)
	require.Equal(t, "alice", got.CreatedBy)
	require.Equal(t, "bob", got.LastModifiedBy)
	require.True(t, edited.LastModifiedAt.Equal(*got.LastModifiedAt))
	require.Equal(t, "completed", got.ParseStatus)
	// Synthetic execution identities never become apparent human contributors.
	system := &types.Knowledge{ID: "system-doc", TenantID: 7, KnowledgeBaseID: "kb", CreatedBy: "forged"}
	require.NoError(t, repo.CreateKnowledge(provenanceContext("system-7", "system"), system))
	require.Empty(t, system.CreatedBy)
	require.Nil(t, system.LastModifiedAt)
}

func TestKnowledgeProvenanceChunkConflictRollbackAndExplicitDeletion(t *testing.T) {
	db := provenanceDB(t)
	knowledge := NewKnowledgeRepository(db)
	chunks := NewChunkRepository(db)
	alice, bob := provenanceContext("alice", "Alice"), provenanceContext("bob", "Bob")
	doc := &types.Knowledge{ID: "doc", TenantID: 7, KnowledgeBaseID: "kb"}
	require.NoError(t, knowledge.CreateKnowledge(alice, doc))
	chunk := &types.Chunk{ID: "chunk", TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", Content: "initial", IsEnabled: true}
	require.NoError(t, chunks.CreateChunks(context.Background(), []*types.Chunk{chunk}))
	before := *doc.LastModifiedAt
	chunk.ContentRevision = 2
	revision := &types.ChunkRevision{ID: uuid.NewString(), TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", ChunkID: "chunk", Revision: 1}
	require.ErrorIs(t, chunks.SaveChunkRevision(bob, chunk, revision, 1), ErrChunkRevisionConflict)
	stored, err := knowledge.GetKnowledgeByID(context.Background(), 7, "doc")
	require.NoError(t, err)
	require.Equal(t, "alice", stored.LastModifiedBy)
	require.True(t, before.Equal(*stored.LastModifiedAt))
	require.NoError(t, chunks.DeleteChunk(types.WithKnowledgeUserMutation(bob), 7, "chunk"))
	stored, err = knowledge.GetKnowledgeByID(context.Background(), 7, "doc")
	require.NoError(t, err)
	require.Equal(t, "bob", stored.LastModifiedBy)
	require.Equal(t, "alice", stored.CreatedBy)
	edited := *stored.LastModifiedAt
	workerChunk := &types.Chunk{ID: "worker-chunk", TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: "doc", Content: "parsed"}
	require.NoError(t, chunks.CreateChunks(context.Background(), []*types.Chunk{workerChunk}))
	require.NoError(t, chunks.DeleteChunksByKnowledgeID(alice, 7, "doc"))
	stored, err = knowledge.GetKnowledgeByID(context.Background(), 7, "doc")
	require.NoError(t, err)
	require.Equal(t, "bob", stored.LastModifiedBy)
	require.True(t, edited.Equal(*stored.LastModifiedAt))
}
