package spaces

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAccountHandoverScopesPreviewAndResumesAfterDisable(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.Exec("UPDATE users SET is_system_admin=TRUE WHERE id='u3'").Error)
	first, err := s.Create(c[0], "项目组", "")
	require.NoError(t, err)
	second, err := s.Create(c[0], "合作组", "")
	require.NoError(t, err)
	other, err := s.Create(c[3], "他人的空间", "")
	require.NoError(t, err)
	join(t, s, c[0], c[1], first.ID, "u2@example.test")
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('shared',1,'已共享'),('private',1,'未共享'),('other',4,'他人的原件')").Error)
	for _, id := range []string{"shared", "private"} {
		require.NoError(t, s.RegisterCreated(c[0], "knowledge_base", id, 1))
	}
	require.NoError(t, s.RegisterCreated(c[3], "knowledge_base", "other", 4))
	require.NoError(t, s.Share(c[0], first.ID, "knowledge_base", "shared", 1, false, true))
	require.NoError(t, s.Share(c[0], second.ID, "knowledge_base", "shared", 1, false, false))
	require.NoError(t, s.Share(c[3], other.ID, "knowledge_base", "other", 4, false, false))
	_, err = s.UserHandover(c[1], "u1")
	require.ErrorIs(t, err, ErrForbidden)
	_, err = s.UserHandover(c[2], "absent")
	require.ErrorIs(t, err, ErrNotFound)
	preview, err := s.UserHandover(c[2], "u1")
	require.NoError(t, err)
	require.True(t, preview.IsActive)
	require.Len(t, preview.Spaces, 2)
	require.Len(t, preview.Resources, 2, "one original with two existing grant paths")
	for _, r := range preview.Resources {
		require.Equal(t, "shared", r.ResourceID)
		require.Equal(t, "u1", r.OwnerUserID)
	}
	require.ErrorIs(t, s.AdminResourceHandover(c[2], "knowledge_base", "shared", 1, first.ID, "u1"), ErrForbidden)
	require.NoError(t, s.db.Exec("UPDATE users SET is_active=FALSE WHERE id='u1'").Error)
	require.ErrorIs(t, s.AdminHandover(c[2], first.ID, "u2", "u4"), ErrConflict)
	require.ErrorIs(t, s.AdminResourceHandover(c[2], "knowledge_base", "shared", 1, first.ID, "u4"), ErrConflict)
	require.ErrorIs(t, s.AdminResourceHandover(c[2], "knowledge_base", "shared", 1, other.ID, "u1"), ErrForbidden)
	preview, err = s.UserHandover(c[2], "u1")
	require.NoError(t, err)
	require.False(t, preview.IsActive)
	require.Len(t, preview.Spaces, 2, "failed transfers must leave ownership unchanged")
	require.NoError(t, s.AdminHandover(c[2], first.ID, "u2", "u1"))
	preview, err = s.UserHandover(c[2], "u1")
	require.NoError(t, err)
	require.Len(t, preview.Spaces, 1, "partial handover can be resumed")
	require.NoError(t, s.AdminHandover(c[2], second.ID, "u2", "u1"))
	require.NoError(t, s.AdminResourceHandover(c[2], "knowledge_base", "shared", 1, first.ID, "u1"))
	preview, err = s.UserHandover(c[2], "u1")
	require.NoError(t, err)
	require.Empty(t, preview.Spaces)
	require.Empty(t, preview.Resources)
	var grants []types.SpaceGrant
	require.NoError(t, s.db.Where("resource_id='shared'").Find(&grants).Error)
	require.Len(t, grants, 2, "all pre-existing grants are retained")
	var resource types.SpaceResource
	require.NoError(t, s.db.First(&resource, "resource_id='shared'").Error)
	require.Empty(t, resource.OwnerUserID)
	require.Equal(t, first.ID, resource.OwnerSpaceID)
	// The ordinary members-page transfer still works for an active owner.
	join(t, s, c[1], c[3], first.ID, "u4@example.test")
	require.NoError(t, s.TransferSpace(c[1], first.ID, "u4"))
}

func TestAccountHandoverDoesNotHideResourceReadFailures(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.Exec("UPDATE users SET is_system_admin=TRUE WHERE id='u3'").Error)
	space, err := s.Create(c[0], "协作组", "")
	require.NoError(t, err)
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'已共享')").Error)
	require.NoError(t, s.RegisterCreated(c[0], "knowledge_base", "kb", 1))
	require.NoError(t, s.Share(c[0], space.ID, "knowledge_base", "kb", 1, false, false))
	require.NoError(t, s.db.Exec("DROP TABLE knowledge_bases").Error)
	_, err = s.UserHandover(c[2], "u1")
	require.Error(t, err, "database failures must not look like an empty handover list")
}
