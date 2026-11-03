package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Inspect the durable wait deadline before a user input wakes the operation.
// This tests real tool execution without sleeping for the configured interval.
func TestCollaborationWaitShortPollUsesConfiguredFloorAndWakesForInput(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		configured, requested, expected int
		restored                        bool
	}{
		{"default", 0, 1000, 30, false},
		{"configured_floor", 12, 1000, 12, false},
		{"longer_request", 12, 45000, 45, false},
		{"lower_bound", 1, 1000, 1, false},
		{"upper_bound", 60, 1000, 60, false},
		{"invalid_falls_back", 61, 1000, 30, false},
		{"resume_keeps_original_deadline", 30, 60000, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {

			_, initial, ctx := collaborationTools(t)
			b, err := initial.Runtime.AcquireParent(ctx, initial.Scope)
			require.NoError(t, err)
			registry := NewToolRegistry()
			for _, tool := range NewCollaborationTools(b, nil, tc.configured) {
				registry.RegisterTool(tool)
			}
			if tc.restored {
				b.WaitUntil = time.Now().Add(2 * time.Second)
			}
			probed := false
			b.HasInput = func(ctx context.Context) (bool, error) {
				g, err := b.Runtime.Store.Read(ctx, b.Scope)
				require.NoError(t, err)
				require.NotNil(t, g.ParentWait)
				require.InDelta(t, tc.expected, time.Until(g.ParentWait.Until).Seconds(), 1)
				probed = true
				return true, nil
			}
			ctx = WithToolExecContext(ctx, &ToolExecContext{
				ToolCallID: "wait", AssistantMessageID: "message",
				Snapshot: func() ([]byte, int, error) { return []byte(`{}`), 1, nil },
			})
			result, err := registry.ExecuteTool(ctx, "wait_agent", json.RawMessage(fmt.Sprintf(`{"timeout_ms":%d}`, tc.requested)))
			require.NoError(t, err)
			require.True(t, probed)
			require.True(t, result.Success)
			require.Contains(t, result.Output, `"interrupted":true`)
			g, err := b.Runtime.Store.Read(ctx, b.Scope)
			require.NoError(t, err)
			require.Nil(t, g.ParentWait, "normal wake clears the checkpoint")
		})
	}
}
