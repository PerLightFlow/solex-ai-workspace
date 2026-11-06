package spaces

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestResourceSourcesMergeOwningSpaceWithoutChangingAccessOrHistory(t *testing.T) {
	for _, earlierGrant := range []bool{true, false} {
		name := "ownership predates grant"
		if earlierGrant {
			name = "grant predates ownership"
		}
		t.Run(name, func(t *testing.T) {
			s, c := fixture(t)
			require.NoError(t, s.db.AutoMigrate(&types.Knowledge{}, &ContentVersion{}))
			require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'项目资料')").Error)
			o, err := s.Create(c[0], "算法组", "")
			require.NoError(t, err)
			join(t, s, c[0], c[1], o.ID, "u2@example.test")
			require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", "editor"))
			// A retained sharing record can coexist with ownership after transfer.
			require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, false))
			require.NoError(t, s.Transfer(c[0], "knowledge_base", "kb", 1, o.ID))
			early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			late := early.Add(2 * time.Hour)
			grantTime, ownerTime := late, early
			if earlierGrant {
				grantTime, ownerTime = early, late
			}
			require.NoError(t, s.db.Model(&types.SpaceGrant{}).Where("resource_id=?", "kb").Update("history_from", grantTime).Error)
			require.NoError(t, s.db.Model(&types.SpaceResource{}).Where("resource_id=?", "kb").Update("created_at", ownerTime).Error)
			require.NoError(t, s.db.Create(&types.Knowledge{ID: "doc", TenantID: 1, KnowledgeBaseID: "kb", Title: "项目文档", Type: "manual", CustomMetadata: types.JSON(`{}`)}).Error)
			for i, created := range []time.Time{early.Add(-time.Hour), early.Add(time.Hour), late.Add(time.Hour)} {
				require.NoError(t, s.db.Create(&ContentVersion{ID: uint64(i + 1), Kind: "knowledge", ResourceID: "doc", TenantID: 1, KBID: "kb", CreatedAt: created}).Error)
			}

			rows, err := s.Resources(c[1], o.ID)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			r := rows[0]
			require.True(t, r.Owned)
			require.True(t, r.Access.Read)
			require.True(t, r.Access.Edit)
			require.True(t, r.Access.Download)
			require.False(t, r.Access.Manage)
			require.Len(t, r.Access.Sources, 1, "ownership and the retained grant must explain the same space only once")
			require.Equal(t, Source{SpaceID: o.ID, SpaceName: "算法组", Role: "editor", AllowEdit: true, AllowDownload: true, HistoryFrom: early}, r.Access.Sources[0])
			versions, _, err := s.Versions(c[1], "doc")
			require.NoError(t, err)
			require.Len(t, versions, 2)
			require.Equal(t, uint64(3), versions[0].ID)
			require.Equal(t, uint64(2), versions[1].ID)
			cutoff, err := s.HistoryCutoff(c[1], "kb")
			require.NoError(t, err)
			require.Equal(t, early, cutoff, "Wiki and document history must retain the earliest permitted version")
			var grants int64
			require.NoError(t, s.db.Model(&types.SpaceGrant{}).Where("resource_id=?", "kb").Count(&grants).Error)
			require.EqualValues(t, 1, grants, "source aggregation must not delete authorization records")

			// Different spaces remain distinct even when their display names match.
			other, err := s.Create(c[0], "算法组", "")
			require.NoError(t, err)
			join(t, s, c[0], c[1], other.ID, "u2@example.test")
			require.NoError(t, s.Share(c[0], other.ID, "knowledge_base", "kb", 1, false, false))
			a, err := s.Check(c[1], "knowledge_base", "kb", 1)
			require.NoError(t, err)
			require.Len(t, a.Sources, 2)
			require.Equal(t, o.ID, a.Sources[0].SpaceID)
			require.Equal(t, other.ID, a.Sources[1].SpaceID)
			require.False(t, a.Sources[1].AllowEdit)
			require.False(t, a.Sources[1].AllowDownload)
			require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", ""))
			a, err = s.Check(c[1], "knowledge_base", "kb", 1)
			require.NoError(t, err)
			require.True(t, a.Read)
			require.False(t, a.Edit)
			require.False(t, a.Download)
			require.False(t, a.Manage)
			require.Len(t, a.Sources, 1)
			require.Equal(t, other.ID, a.Sources[0].SpaceID)
			require.NoError(t, s.Unshare(c[0], other.ID, "knowledge_base", "kb", 1))
			a, err = s.Check(c[1], "knowledge_base", "kb", 1)
			require.NoError(t, err)
			require.False(t, a.Read)
			require.Empty(t, a.Sources)
		})
	}
}
