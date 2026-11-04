/** Server-side validation is authoritative; these bounds give the same feedback before saving. */
export const AGENT_RUNTIME_SETTING_KEYS = [
  'agent.file_generation_sandbox_id',
  'agent.max_iterations',
  'agent.max_completion_tokens',
  'agent.collaboration_wait_timeout_seconds',
] as const

const INTEGER_BOUNDS: Record<string, { min: number; max: number }> = {
  'agent.max_completion_tokens': { min: 8192, max: 65536 },
  'agent.max_iterations': { min: 1, max: 200 },
  'agent.collaboration_wait_timeout_seconds': { min: 1, max: 60 },
}

export function agentRuntimeSettingBounds(key: string): { min: number; max: number } | undefined {
  return Object.hasOwn(INTEGER_BOUNDS, key) ? INTEGER_BOUNDS[key] : undefined
}

export function isValidAgentRuntimeSettingValue(key: string, value: unknown): boolean {
  const bounds = agentRuntimeSettingBounds(key)
  return !bounds || (typeof value === 'number' && Number.isInteger(value) && value >= bounds.min && value <= bounds.max)
}
