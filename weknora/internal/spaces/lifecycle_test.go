package spaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestArchiveAndAdministratorHandover(t *testing.T) {
	s, c := fixture(t)
	o, e := s.Create(c[0], "项目组", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", "editor"))
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'共享文件')").Error)
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, true, true))
	require.ErrorIs(t, s.Archive(c[1], o.ID, true), ErrForbidden)
	require.NoError(t, s.Archive(c[0], o.ID, true))
	a, e := s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.True(t, a.Read)
	require.False(t, a.Edit)
	_, e = s.Invite(c[0], o.ID, "u3@example.test")
	require.ErrorIs(t, e, ErrConflict)
	require.NoError(t, s.Archive(c[0], o.ID, false))
	a, e = s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.True(t, a.Edit)
	require.NoError(t, s.db.Exec("UPDATE users SET is_system_admin=TRUE WHERE id='u3'").Error)
	require.ErrorIs(t, s.AdminHandover(c[2], o.ID, "u2"), ErrInvalid)
	require.NoError(t, s.db.Exec("UPDATE users SET is_active=FALSE WHERE id='u1'").Error)
	require.ErrorIs(t, s.AdminHandover(c[1], o.ID, "u2"), ErrForbidden)
	require.NoError(t, s.AdminHandover(c[2], o.ID, "u2"))
	rows, e := s.List(c[1])
	require.NoError(t, e)
	require.Equal(t, "owner", rows[0].Role)
	require.ErrorIs(t, s.RequireAccountDeletionReady(c[2], "u2"), ErrConflict)
}

func TestKnowledgeBaseRecycleRestoresOnlyItsLiveChildren(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &RecycledContent{}, &ContentVersion{}, &Retention{}, &RetainedFile{}))
	require.NoError(t, s.db.Exec("CREATE TABLE wiki_pages(id TEXT PRIMARY KEY,knowledge_base_id TEXT,updated_at DATETIME,deleted_at DATETIME)").Error)
	require.NoError(t, s.db.Exec("CREATE TABLE chunks(id TEXT PRIMARY KEY,knowledge_base_id TEXT,updated_at DATETIME,deleted_at DATETIME)").Error)
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'项目资料')").Error)
	k := types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "kb", Title: "文件", FilePath: "local://example/source.pdf"}
	require.NoError(t, s.db.Create(&k).Error)
	require.NoError(t, s.db.Exec("INSERT INTO chunks(id,knowledge_base_id) VALUES('live','kb')").Error)
	require.NoError(t, s.db.Exec("INSERT INTO chunks(id,knowledge_base_id,deleted_at) VALUES('old','kb',?)", time.Now().Add(-time.Hour)).Error)
	require.NoError(t, s.RecycleResource(c[0], "knowledge_base", "kb", 1))
	_, e := s.Check(c[0], "knowledge_base", "kb", 1)
	require.ErrorIs(t, e, ErrNotFound)
	var removed []string
	require.NoError(t, s.SweepRetention(context.Background(), func(_ context.Context, _ *types.KnowledgeBase, path string) error {
		removed = append(removed, path)
		return nil
	}))
	require.Empty(t, removed, "unexpired whole-KB recycle must retain child source files")
	require.ErrorIs(t, s.RestoreResource(c[1], "knowledge_base", "kb", 1), ErrForbidden)
	require.NoError(t, s.RestoreResource(c[0], "knowledge_base", "kb", 1))
	require.NoError(t, s.db.First(&k, "id='doc'").Error)
	var n int64
	require.NoError(t, s.db.Table("chunks").Where("knowledge_base_id='kb' AND deleted_at IS NULL").Count(&n).Error)
	require.EqualValues(t, 1, n)
}

func TestRetentionDoesNotDeleteReferencedSourceAndRetriesFailures(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &RecycledContent{}, &ContentVersion{}, &Retention{}, &RetainedFile{}))
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'资料')").Error)
	k := types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "kb", FilePath: "local://retained.pdf", Title: "source"}
	require.NoError(t, s.db.Create(&k).Error)
	require.NoError(t, s.PreserveKnowledge(c[0], []*types.Knowledge{&k}))
	require.NoError(t, s.db.Delete(&k).Error)
	count := 0
	remove := func(context.Context, *types.KnowledgeBase, string) error { count++; return nil }
	require.NoError(t, s.SweepRetention(c[0], remove))
	require.Zero(t, count)
	require.NoError(t, s.db.Model(&RecycledContent{}).Where("resource_id='doc'").Update("expires_at", time.Now().Add(-time.Hour)).Error)
	require.ErrorIs(t, s.SweepRetention(c[0], func(context.Context, *types.KnowledgeBase, string) error { return ErrConflict }), ErrConflict)
	require.NoError(t, s.SweepRetention(c[0], remove))
	require.Equal(t, 1, count)
	_, e := s.ReviveKnowledge(c[0], "doc")
	require.ErrorIs(t, e, ErrNotFound)
}

func TestResourceBearerLinkRevokesWithOriginalAccess(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &types.StoredResource{}, &types.ResourceBinding{}, &types.ResourceAccessGrant{}, &ResourceLinkPrincipal{}))
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'资料')").Error)
	require.NoError(t, s.db.Create(&types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "kb"}).Error)
	r := types.StoredResource{ID: "resource", Handle: "handle", TenantID: 1, PhysicalPath: "local://source", LocationHash: "location"}
	require.NoError(t, s.db.Create(&r).Error)
	require.NoError(t, s.db.Create(&types.ResourceBinding{ResourceID: r.ID, TenantID: 1, OwnerType: types.ResourceOwnerKnowledge, OwnerID: "doc", Relation: types.ResourceRelationSourceFile}).Error)
	o, e := s.Create(c[0], "共享", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, false))
	require.ErrorIs(t, s.IssueResourceLink(c[1], &r, "no-download", time.Hour), ErrForbidden)
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, true))
	require.NoError(t, s.IssueResourceLink(c[1], &r, "allowed", time.Hour))
	_, e = s.ResolveResourceLink(context.Background(), "allowed")
	require.NoError(t, e)
	require.NoError(t, s.Unshare(c[0], o.ID, "knowledge_base", "kb", 1))
	_, e = s.ResolveResourceLink(context.Background(), "allowed")
	require.ErrorIs(t, e, ErrForbidden)
}

func TestHistoryPreservesManualImagesAndIndependentMessageClaims(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &RecycledContent{}, &ContentVersion{}, &Retention{}, &RetainedFile{}))
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'资料')").Error)
	ref := "resource://abcdefghijklmnopqrstuv"
	resource := types.StoredResource{ID: "image", Handle: "abcdefghijklmnopqrstuv", TenantID: 1, LocationHash: "image", PhysicalPath: "local://1/image.png"}
	require.NoError(t, s.db.Create(&resource).Error)
	k := types.Knowledge{ID: "manual", TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", Title: "illustrated"}
	require.NoError(t, k.SetManualMetadata(types.NewManualKnowledgeMetadata("![example]("+ref+")", "draft", 1)))
	require.NoError(t, s.db.Create(&k).Error)
	require.NoError(t, s.db.Create(&types.ResourceBinding{ResourceID: "image", TenantID: 1, OwnerType: "knowledge", OwnerID: k.ID, Relation: "attachment"}).Error)
	require.NoError(t, s.PreserveKnowledge(c[0], []*types.Knowledge{&k}))
	require.NoError(t, s.db.Delete(&k).Error)
	retained, e := s.FileRetained(c[0], ref)
	require.NoError(t, e)
	require.True(t, retained)
	require.NoError(t, s.db.Create(&types.ResourceBinding{ResourceID: "image", TenantID: 1, OwnerType: "message", OwnerID: "private-answer", Relation: "artifact"}).Error)
	require.NoError(t, s.db.Model(&RecycledContent{}).Where("resource_id='manual'").Update("expires_at", time.Now().Add(-time.Hour)).Error)
	count := 0
	remove := func(context.Context, *types.KnowledgeBase, string) error { count++; return nil }
	require.NoError(t, s.SweepRetention(c[0], remove))
	require.Zero(t, count)
	require.NoError(t, s.db.Where("owner_type='message'").Delete(&types.ResourceBinding{}).Error)
	require.NoError(t, s.SweepRetention(c[0], remove))
	require.Equal(t, 1, count)
}
