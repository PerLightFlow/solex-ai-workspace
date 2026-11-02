import type { CustomAgent, CustomAgentConfig } from '../api/agent'
import { managedAgentConfig } from './managedRuntime'

/** Global templates resolve data and authorizations as the caller at runtime. */
export function systemAgentConfig(config: CustomAgentConfig): CustomAgentConfig {
  const result = managedAgentConfig(config, true)
  result.kb_selection_mode = config.kb_selection_mode === 'none' ? 'none' : 'all'
  result.mcp_selection_mode = config.mcp_selection_mode === 'all' ? 'all' : 'none'
  result.skills_selection_mode = config.skills_selection_mode === 'all' ? 'all' : 'none'
  result.knowledge_bases = []
  result.mcp_services = []
  result.selected_skills = []
  // Older server payloads can contain document-level bindings too.
  delete (result as Record<string, unknown>).knowledge_ids
  return result
}

/** Never invent a built-in after the server has removed it. */
export function resolveSelectedAgent(
  agents: readonly CustomAgent[], id: string, sourceTenantId: string | null | undefined,
  shared: readonly { source_tenant_id: string | number; agent: Partial<CustomAgent> }[],
): CustomAgent | undefined {
  if (sourceTenantId) {
    return shared.find(item => item.agent.id === id && String(item.source_tenant_id) === sourceTenantId)?.agent as CustomAgent | undefined
  }
  return agents.find(agent => agent.id === id && !agent.is_internal)
}

export function canManageAgentResource(
  agent: Pick<CustomAgent, 'is_builtin' | 'created_by'>,
  auth: { isSystemAdmin: boolean; user?: { id?: string } | null; hasRole: (role: 'contributor' | 'admin') => boolean },
  managedDeployment = true,
): boolean {
  if (agent.is_builtin && managedDeployment) return auth.isSystemAdmin
  return Boolean(agent.created_by && agent.created_by === auth.user?.id && auth.hasRole('contributor')) || auth.hasRole('admin')
}

export function agentUsesSystemModels(agent: Pick<CustomAgent, 'is_builtin'>, managedDeployment: boolean, managedUser: boolean): boolean {
  return managedUser || (managedDeployment && agent.is_builtin)
}
