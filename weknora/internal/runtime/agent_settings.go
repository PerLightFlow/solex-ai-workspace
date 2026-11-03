package runtime

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	AgentMaxIterationsKey                   = "agent.max_iterations"
	AgentCollaborationWaitTimeoutSecondsKey = "agent.collaboration_wait_timeout_seconds"
	DefaultAgentMaxIterations               = 50
	MaxAgentMaxIterations                   = 200
	DefaultCollaborationWaitTimeoutSeconds  = 30
	MaxCollaborationWaitTimeoutSeconds      = 60
)

// IntegerSettingReader keeps execution policy independent of the settings
// service implementation and its management API.
type IntegerSettingReader interface {
	GetInt(context.Context, string, string, int64) int64
}

// ResolveAgentExecutionSettings is called once when new work is built. Bounds
// defend against legacy/out-of-band database writes as well as malformed API
// input (the management service separately rejects invalid values on save).
func ResolveAgentExecutionSettings(ctx context.Context, reader IntegerSettingReader) types.AgentExecutionSettings {
	return types.AgentExecutionSettings{
		MaxIterations: boundedSettingInt(ctx, reader, AgentMaxIterationsKey,
			DefaultAgentMaxIterations, MaxAgentMaxIterations),
		CollaborationWaitTimeoutSeconds: boundedSettingInt(ctx, reader, AgentCollaborationWaitTimeoutSecondsKey,
			DefaultCollaborationWaitTimeoutSeconds, MaxCollaborationWaitTimeoutSeconds),
	}
}

func boundedSettingInt(ctx context.Context, reader IntegerSettingReader, key string, fallback, maximum int) int {
	if reader == nil {
		return fallback
	}
	value := reader.GetInt(ctx, key, "", int64(fallback))
	if value < 1 || value > int64(maximum) {
		return fallback
	}
	return int(value)
}
