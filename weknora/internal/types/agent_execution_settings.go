package types

// AgentExecutionSettings is the non-secret platform budget pinned when a run
// starts. Delegated work and approval resumes retain this snapshot even when
// an administrator changes the settings while that work is pending.
type AgentExecutionSettings struct {
	MaxCompletionTokens             int `json:"max_completion_tokens,omitempty"`
	MaxIterations                   int `json:"max_iterations"`
	CollaborationWaitTimeoutSeconds int `json:"collaboration_wait_timeout_seconds"`
}

func AgentExecutionSettingsFromConfig(cfg *AgentConfig) *AgentExecutionSettings {
	if cfg == nil {
		return nil
	}
	return &AgentExecutionSettings{
		MaxCompletionTokens:             cfg.MaxCompletionTokens,
		MaxIterations:                   cfg.MaxIterations,
		CollaborationWaitTimeoutSeconds: cfg.CollaborationWaitTimeoutSeconds,
	}
}

func (s *AgentExecutionSettings) Apply(cfg *AgentConfig) {
	if s == nil || cfg == nil {
		return
	}
	cfg.MaxIterations = s.MaxIterations
	// Old snapshots have no output budget. Keep the newly resolved default.
	if s.MaxCompletionTokens > 0 {
		cfg.MaxCompletionTokens = s.MaxCompletionTokens
	}
	cfg.CollaborationWaitTimeoutSeconds = s.CollaborationWaitTimeoutSeconds
}
