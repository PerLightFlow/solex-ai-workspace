package types

// AgentExecutionSettings is the non-secret platform budget pinned when a run
// starts. Delegated work and approval resumes retain this snapshot even when
// an administrator changes the settings while that work is pending.
type AgentExecutionSettings struct {
	MaxIterations                   int `json:"max_iterations"`
	CollaborationWaitTimeoutSeconds int `json:"collaboration_wait_timeout_seconds"`
}

func AgentExecutionSettingsFromConfig(cfg *AgentConfig) *AgentExecutionSettings {
	if cfg == nil {
		return nil
	}
	return &AgentExecutionSettings{
		MaxIterations:                   cfg.MaxIterations,
		CollaborationWaitTimeoutSeconds: cfg.CollaborationWaitTimeoutSeconds,
	}
}

func (s *AgentExecutionSettings) Apply(cfg *AgentConfig) {
	if s == nil || cfg == nil {
		return
	}
	cfg.MaxIterations = s.MaxIterations
	cfg.CollaborationWaitTimeoutSeconds = s.CollaborationWaitTimeoutSeconds
}
