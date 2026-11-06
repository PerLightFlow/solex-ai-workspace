package spaces

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInviteCandidatesRequireLiveSpaceManager(t *testing.T) {
	s, c := fixture(t)
	o, err := s.Create(c[0], "研发", "")
	require.NoError(t, err)
	_, err = s.InviteCandidates(c[1], o.ID, "", 0, 20)
	require.ErrorIs(t, err, ErrForbidden)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	for _, role := range []string{"viewer", "editor"} {
		require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", role))
		_, err = s.InviteCandidates(c[1], o.ID, "", 0, 20)
		require.ErrorIs(t, err, ErrForbidden)
		_, err = s.InviteUser(c[1], o.ID, "u3")
		require.ErrorIs(t, err, ErrForbidden)
	}
	require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", "admin"))
	page, err := s.InviteCandidates(c[1], o.ID, "", 0, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	_, err = s.InviteUser(c[1], o.ID, "u3")
	require.NoError(t, err)
	require.NoError(t, s.Archive(c[0], o.ID, true))
	_, err = s.InviteCandidates(c[0], o.ID, "", 0, 20)
	require.ErrorIs(t, err, ErrConflict)
	_, err = s.InviteUser(c[0], o.ID, "u4")
	require.ErrorIs(t, err, ErrConflict)
}

func TestInviteCandidatesFilterSearchAndPaginateAccounts(t *testing.T) {
	s, c := fixture(t)
	o, err := s.Create(c[0], "研发", "")
	require.NoError(t, err)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.db.Exec("UPDATE users SET is_active=FALSE WHERE id='u3'").Error)
	require.NoError(t, s.db.Exec("UPDATE users SET deleted_at=? WHERE id='u4'", time.Now()).Error)
	for _, row := range []InviteCandidate{
		{ID: "a", Username: "Alice", Email: "Alice@example.test"},
		{ID: "b", Username: "研发_50%", Email: "bob@example.test"},
		{ID: "c", Username: "Carol", Email: "carol@example.test"},
	} {
		require.NoError(t, s.db.Exec("INSERT INTO users(id,tenant_id,username,email,is_active) VALUES(?,1,?,?,TRUE)", row.ID, row.Username, row.Email).Error)
	}
	i, err := s.Invite(c[0], o.ID, "alice@example.test")
	require.NoError(t, err)
	page, err := s.InviteCandidates(c[0], o.ID, "", 0, 1)
	require.NoError(t, err)
	require.Equal(t, "c", page.Items[0].ID)
	require.True(t, page.HasMore)
	page, err = s.InviteCandidates(c[0], o.ID, "", 1, 1)
	require.NoError(t, err)
	require.Equal(t, "b", page.Items[0].ID)
	require.False(t, page.HasMore)
	for _, search := range []string{"_50%", "BOB@EXAMPLE", "研发"} {
		page, err = s.InviteCandidates(c[0], o.ID, search, 0, 20)
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		require.Equal(t, "b", page.Items[0].ID)
	}
	for _, search := range []string{"nobody", "' OR 1=1 --"} {
		page, err = s.InviteCandidates(c[0], o.ID, search, 0, 20)
		require.NoError(t, err)
		require.Empty(t, page.Items)
	}
	// Expired and revoked invitations must not prevent a new invitation.
	for _, updates := range []map[string]any{
		{"expires_at": time.Now().Add(-time.Minute)},
		{"expires_at": time.Now().Add(time.Hour), "status": "revoked"},
	} {
		require.NoError(t, s.db.Model(i).Updates(updates).Error)
		page, err = s.InviteCandidates(c[0], o.ID, "alice", 0, 20)
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
	}
	encoded, err := json.Marshal(page.Items[0])
	require.NoError(t, err)
	var identity map[string]any
	require.NoError(t, json.Unmarshal(encoded, &identity))
	require.Len(t, identity, 3)
	require.Equal(t, "a", identity["id"])
	require.Equal(t, "Alice", identity["username"])
	require.Equal(t, "Alice@example.test", identity["email"])
	for _, pagination := range [][2]int{{-1, 20}, {100001, 20}, {0, 0}, {0, 51}} {
		_, err = s.InviteCandidates(c[0], o.ID, "", pagination[0], pagination[1])
		require.ErrorIs(t, err, ErrInvalid)
	}
}

func TestInviteSelectedUserRechecksStatusMembershipAndPendingInvitation(t *testing.T) {
	s, c := fixture(t)
	o, err := s.Create(c[0], "研发", "")
	require.NoError(t, err)
	_, err = s.InviteUser(c[0], o.ID, "u1")
	require.ErrorIs(t, err, ErrConflict)
	_, err = s.InviteUser(c[0], o.ID, "missing")
	require.ErrorIs(t, err, ErrConflict)
	_, err = s.InviteUser(c[0], o.ID, "")
	require.ErrorIs(t, err, ErrInvalid)
	i, err := s.InviteUser(c[0], o.ID, "u2")
	require.NoError(t, err)
	require.Equal(t, "u2@example.test", i.Email)
	_, err = s.InviteUser(c[0], o.ID, "u2")
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, s.Accept(c[2], i.ID), ErrNotFound)
	require.NoError(t, s.Accept(c[1], i.ID))
	var member types.SpaceMember
	require.NoError(t, s.db.First(&member, "space_id=? AND user_id=?", o.ID, "u2").Error)
	require.Equal(t, "viewer", member.Role)
	_, err = s.InviteUser(c[0], o.ID, "u2")
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, s.db.Exec("UPDATE users SET is_active=FALSE WHERE id='u3'").Error)
	_, err = s.InviteUser(c[0], o.ID, "u3")
	require.ErrorIs(t, err, ErrConflict)
	require.NoError(t, s.db.Exec("UPDATE users SET deleted_at=? WHERE id='u4'", time.Now()).Error)
	_, err = s.InviteUser(c[0], o.ID, "u4")
	require.ErrorIs(t, err, ErrConflict)
}
