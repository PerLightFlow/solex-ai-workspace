package spaces

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) (*Service, []context.Context) {
	t.Helper()
	db, e := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, e)
	// Explicit minimal production-shaped tables avoid PostgreSQL-only model defaults.
	for _, sql := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id INTEGER, username TEXT, email TEXT, is_active BOOLEAN, is_system_admin BOOLEAN DEFAULT FALSE, deleted_at DATETIME)`,
		`CREATE TABLE organizations (id TEXT PRIMARY KEY, name TEXT, description TEXT, avatar TEXT, owner_id TEXT, owner_tenant_id INTEGER, invite_code TEXT, invite_code_expires_at DATETIME, invite_code_validity_days INTEGER, require_approval BOOLEAN, searchable BOOLEAN, member_limit INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, name TEXT, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE custom_agents (id TEXT,tenant_id INTEGER,name TEXT,config TEXT,deleted_at DATETIME,PRIMARY KEY(id,tenant_id))`,
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	require.NoError(t, db.AutoMigrate(&types.SpaceMember{}, &types.SpaceGrant{}, &types.SpaceResource{}, &types.SpaceInvitation{}, &types.SpaceEvent{}, &SpaceState{}, &types.StoredResource{}, &types.ResourceBinding{}))
	contexts := []context.Context{}
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("u%d", i)
		require.NoError(t, db.Exec("INSERT INTO users(id,tenant_id,username,email,is_active) VALUES(?,?,?,?,?)", id, i, id, id+"@example.test", true).Error)
		contexts = append(contexts, types.WithCaller(context.Background(), types.Caller{UserID: id, TenantID: uint64(i), Role: types.TenantRoleOwner}))
	}
	return New(db), contexts
}

func join(t *testing.T, s *Service, owner, user context.Context, space, email string) {
	t.Helper()
	i, e := s.Invite(owner, space, email)
	require.NoError(t, e)
	require.NoError(t, s.Accept(user, i.ID))
}

func TestInvitationBindsAccountAndCannotGrantViaPersonalOwner(t *testing.T) {
	s, c := fixture(t)
	o, e := s.Create(c[0], "算法组", "")
	require.NoError(t, e)
	i, e := s.Invite(c[0], o.ID, "u2@example.test")
	require.NoError(t, e)
	require.ErrorIs(t, s.Accept(c[2], i.ID), ErrNotFound)
	require.NoError(t, s.Accept(c[1], i.ID))
	require.ErrorIs(t, s.Accept(c[1], i.ID), ErrNotFound)
	m, e := s.Members(c[1], o.ID)
	require.NoError(t, e)
	require.Len(t, m, 2)
	require.Equal(t, "viewer", m[1].Role)
	_, e = s.Invite(c[1], o.ID, "u3@example.test")
	require.ErrorIs(t, e, ErrForbidden)
	spoof := types.WithCaller(c[1], types.Caller{UserID: "u2", TenantID: 1, Role: types.TenantRoleOwner})
	_, e = s.List(spoof)
	require.ErrorIs(t, e, ErrForbidden)
}

func TestEditAndDownloadCapsMultiplePathsAndRevocation(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'项目资料')").Error)
	o, e := s.Create(c[0], "算法组", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, false))
	require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", "editor"))
	a, e := s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.True(t, a.Read)
	require.False(t, a.Edit)
	require.False(t, a.Download)
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, true, true))
	a, e = s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.True(t, a.Edit)
	require.True(t, a.Download)
	require.ErrorIs(t, s.Share(c[1], o.ID, "knowledge_base", "kb", 1, true, true), ErrForbidden)
	o2, e := s.Create(c[0], "全员", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o2.ID, "u2@example.test")
	require.NoError(t, s.Share(c[0], o2.ID, "knowledge_base", "kb", 1, false, false))
	require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", ""))
	a, e = s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.True(t, a.Read)
	require.False(t, a.Edit)
	require.NoError(t, s.Unshare(c[0], o2.ID, "knowledge_base", "kb", 1))
	a, e = s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.False(t, a.Read)
}

func TestTransferredResourceDoesNotBelongToOriginalStorageOwner(t *testing.T) {
	s, c := fixture(t)
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'项目资料')").Error)
	o, e := s.Create(c[0], "研发", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.Transfer(c[0], "knowledge_base", "kb", 1, o.ID))
	require.NoError(t, s.TransferSpace(c[0], o.ID, "u2"))
	require.NoError(t, s.SetMemberRole(c[0], o.ID, "u1", ""))
	a, e := s.Check(c[0], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.False(t, a.Read)
	a, e = s.Check(c[1], "knowledge_base", "kb", 1)
	require.NoError(t, e)
	require.True(t, a.Manage)
	require.ErrorIs(t, s.Delete(c[1], o.ID), ErrInvalid)
}

func TestAdminCannotPromotePeersOrTakeOwnership(t *testing.T) {
	s, c := fixture(t)
	o, e := s.Create(c[0], "研发", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	join(t, s, c[0], c[2], o.ID, "u3@example.test")
	require.NoError(t, s.SetMemberRole(c[0], o.ID, "u2", "admin"))
	require.ErrorIs(t, s.SetMemberRole(c[1], o.ID, "u3", "admin"), ErrForbidden)
	require.ErrorIs(t, s.TransferSpace(c[1], o.ID, "u3"), ErrForbidden)
	require.ErrorIs(t, s.SetMemberRole(c[0], o.ID, "u1", ""), ErrInvalid)
}

func TestAgentCannotExposeAllOrUnsharedKnowledge(t *testing.T) {
	s, c := fixture(t)
	o, e := s.Create(c[0], "研发", "")
	require.NoError(t, e)
	join(t, s, c[0], c[1], o.ID, "u2@example.test")
	require.NoError(t, s.db.Exec(`INSERT INTO custom_agents(id,tenant_id,name,config) VALUES('agent',1,'助手','{"kb_selection_mode":"all"}')`).Error)
	require.ErrorIs(t, s.Share(c[0], o.ID, "agent", "agent", 1, false, false), ErrInvalid)
	require.NoError(t, s.db.Exec(`UPDATE custom_agents SET config='{"kb_selection_mode":"selected","knowledge_bases":["kb"]}'`).Error)
	require.ErrorIs(t, s.Share(c[0], o.ID, "agent", "agent", 1, false, false), ErrInvalid)
	require.NoError(t, s.db.Exec("INSERT INTO knowledge_bases(id,tenant_id,name) VALUES('kb',1,'资料')").Error)
	require.NoError(t, s.Share(c[0], o.ID, "knowledge_base", "kb", 1, false, false))
	require.NoError(t, s.Share(c[0], o.ID, "agent", "agent", 1, false, false))
	_, e = s.Agent(c[1], "agent", 1)
	require.NoError(t, e)
	require.NoError(t, s.Unshare(c[0], o.ID, "knowledge_base", "kb", 1))
	_, e = s.Agent(c[1], "agent", 1)
	require.ErrorIs(t, e, ErrForbidden)
}

func TestExpiredInvitationDoesNotEnroll(t *testing.T) {
	s, c := fixture(t)
	o, e := s.Create(c[0], "研发", "")
	require.NoError(t, e)
	i, e := s.Invite(c[0], o.ID, "u2@example.test")
	require.NoError(t, e)
	require.NoError(t, s.db.Model(i).Update("expires_at", time.Now().Add(-time.Minute)).Error)
	require.ErrorIs(t, s.Accept(c[1], i.ID), ErrNotFound)
	rows, e := s.List(c[1])
	require.NoError(t, e)
	require.Empty(t, rows)
}
