package collaboration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseBudgetDistinguishesStoppingFromCancelledAndCompleted(t *testing.T) {
	store, scope := testStore(t)
	r := New(store, nil)
	b, err := r.AcquireParent(t.Context(), scope)
	require.NoError(t, err)
	for _, name := range []string{"done", "running", "queued"} {
		_, err := r.Spawn(b.Context(t.Context()), scope, name, name, "template", "{}", "task")
		require.NoError(t, err)
	}
	require.NoError(t, store.Change(b.Context(t.Context()), scope, func(g *Group) error {
		done := current(g.Members[0])
		done.State = "succeeded"
		done.Result.Answer = "committed evidence"
		publish(g, g.Members[0], done)
		running := current(g.Members[1])
		running.State, running.Owner, running.Lease = "running", "worker", time.Now().Add(time.Minute)
		return nil
	}))
	closed, err := b.CloseBudget(t.Context())
	require.NoError(t, err)
	require.Len(t, closed.Tasks, 3)
	require.Equal(t, "succeeded", closed.Tasks[0].Status)
	require.False(t, closed.Tasks[0].CancelRequested)
	require.Equal(t, "running", closed.Tasks[1].Status)
	require.True(t, closed.Tasks[1].CancelRequested, "requesting cancellation does not prove a running tool stopped")
	require.Equal(t, "cancelled", closed.Tasks[2].Status)
	require.Len(t, closed.Events, 2, "completed evidence and cancellation are captured atomically")
	require.Contains(t, closed.Events[0].Content, "committed evidence")
	require.Equal(t, "cancelling", read(t, store, scope).Status)
	_, err = r.Spawn(b.Context(t.Context()), scope, "new", "new", "template", "{}", "task")
	require.ErrorIs(t, err, ErrClosed)
}

func TestCloseBudgetRejectsExpiredOrReplacedParent(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "replaced", true: "expired"}[expired], func(t *testing.T) {
			store, scope := testStore(t)
			r := New(store, nil)
			b, err := r.AcquireParent(t.Context(), scope)
			require.NoError(t, err)
			_, err = r.Spawn(b.Context(t.Context()), scope, "one", "one", "template", "{}", "task")
			require.NoError(t, err)
			require.NoError(t, store.Change(t.Context(), scope, func(g *Group) error {
				if expired {
					g.ParentLease = time.Now().Add(-time.Second)
				} else {
					g.ParentOwner = "replacement"
				}
				return nil
			}))
			closed, err := b.CloseBudget(t.Context())
			require.ErrorIs(t, err, ErrConflict)
			require.Empty(t, closed.Tasks)
			require.Empty(t, closed.Events)
			g := read(t, store, scope)
			require.Equal(t, "active", g.Status)
			require.False(t, current(g.Members[0]).CancelRequested)
		})
	}
}
