package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type executionSettingValues map[string]int64

func (v executionSettingValues) GetInt(_ context.Context, key, _ string, fallback int64) int64 {
	if value, ok := v[key]; ok {
		return value
	}
	return fallback
}

func TestAgentExecutionSettingsBoundInvalidStoredValues(t *testing.T) {
	ctx := context.Background()
	defaults := ResolveAgentExecutionSettings(ctx, nil)
	require.Equal(t, 50, defaults.MaxIterations)
	require.Equal(t, 30, defaults.CollaborationWaitTimeoutSeconds)
	for _, value := range []int64{-1, 0, 201, 1 << 62} {
		settings := ResolveAgentExecutionSettings(ctx, executionSettingValues{AgentMaxIterationsKey: value})
		require.Equal(t, defaults.MaxIterations, settings.MaxIterations)
	}
	for _, value := range []int64{-1, 0, 61, 1 << 62} {
		settings := ResolveAgentExecutionSettings(ctx, executionSettingValues{AgentCollaborationWaitTimeoutSecondsKey: value})
		require.Equal(t, defaults.CollaborationWaitTimeoutSeconds, settings.CollaborationWaitTimeoutSeconds)
	}
	for _, values := range [][2]int64{{1, 1}, {200, 60}, {80, 45}} {
		settings := ResolveAgentExecutionSettings(ctx, executionSettingValues{
			AgentMaxIterationsKey: values[0], AgentCollaborationWaitTimeoutSecondsKey: values[1],
		})
		require.EqualValues(t, values[0], settings.MaxIterations)
		require.EqualValues(t, values[1], settings.CollaborationWaitTimeoutSeconds)
	}
}
