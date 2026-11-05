package spaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestContentHistoryAtomicConflictAndRestoreBoundary(t *testing.T) {
	t.Setenv("SOLEX_USER_SPACES", "true")
	s, c := fixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &ContentVersion{}, &RecycledContent{}, &Retention{}, &RetainedFile{}))
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name)VALUES('kb',1,'资料')").Error)
	k := types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "kb", Title: "初版", Type: "manual", CustomMetadata: types.JSON(`{}`)}
	require.NoError(t, k.SetManualMetadata(types.NewManualKnowledgeMetadata("初版正文", "draft", 1)))
	require.NoError(t, s.db.Create(&k).Error)
	old := KnowledgeRevision(&k)
	write := func(title string) func(*gorm.DB) error {
		return func(tx *gorm.DB) error {
			return tx.Model(&types.Knowledge{}).Where("id='doc'").Update("title", title).Error
		}
	}
	require.NoError(t, MutateKnowledge(types.WithExpectedContentVersion(c[0], old), s.db, "doc", write("第二版")))
	require.ErrorIs(t, MutateKnowledge(types.WithExpectedContentVersion(c[0], old), s.db, "doc", write("覆盖")), ErrConflict)
	versions, revision, e := s.Versions(c[0], "doc")
	require.NoError(t, e)
	require.Len(t, versions, 2)
	require.NotEqual(t, old, revision)
	require.Error(t, MutateKnowledge(c[0], s.db, "doc", func(tx *gorm.DB) error { require.NoError(t, write("不可见")(tx)); return ErrInvalid }))
	versions, _, e = s.Versions(c[0], "doc")
	require.NoError(t, e)
	require.Len(t, versions, 2)
	o, e := s.Create(c[0], "共享", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, false))
	versions, _, e = s.Versions(c[1], "doc")
	require.NoError(t, e)
	require.Empty(t, versions, "old private revisions must not become readable")
	require.ErrorIs(t, MutateKnowledge(c[1], s.db, "doc", write("越权")), ErrForbidden)
	require.NoError(t, s.db.First(&k, "id='doc'").Error)
	require.NoError(t, s.PreserveKnowledge(c[0], []*types.Knowledge{&k}))
	require.NoError(t, s.db.Delete(&k).Error)
	_, e = s.ReviveKnowledge(c[1], "doc")
	require.ErrorIs(t, e, ErrForbidden)
	restored, e := s.ReviveKnowledge(c[0], "doc")
	require.NoError(t, e)
	require.Equal(t, "第二版", restored.Title)
	var live types.Knowledge
	require.NoError(t, s.db.First(&live, "id='doc'").Error)
	require.Equal(t, "disabled", live.EnableStatus)
}

func TestRunningAgentStopsWhenKnowledgeGrantIsRevoked(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name)VALUES('kb',1,'资料')").Error)
	o, e := s.Create(c[0], "共享", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, false))
	run, stop, e := s.GuardRun(c[1], nil, []string{"kb"})
	require.NoError(t, e)
	defer stop()
	require.NoError(t, s.Unshare(c[0], o.ID, "knowledge_base", "kb", 1))
	require.ErrorIs(t, types.CheckRunAuthorization(run), ErrForbidden)
	select {
	case <-run.Done():
		require.ErrorIs(t, run.Err(), context.Canceled)
	case <-time.After(4 * time.Second):
		t.Fatal("revoked in-flight run was not cancelled")
	}
}
