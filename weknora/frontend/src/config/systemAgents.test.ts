import assert from 'node:assert/strict'
import test from 'node:test'
import { reactive } from 'vue'
import type { CustomAgent } from '../api/agent'
import { systemAgentConfig, canManageAgentResource, resolveSelectedAgent, agentUsesSystemModels } from './systemAgents'

const agent = (data: Partial<CustomAgent> = {}): CustomAgent => ({ id: 'builtin-global', name: 'Global', is_builtin: true, config: {}, ...data })

test('global templates preserve business behavior without binding administrator private resources', () => {
  const original = reactive({ model_id: 'private-model', sandbox_config_id: 'private-sandbox', temperature: .3,
    system_prompt: 'Help hardware projects', allowed_tools: ['web_search'], web_search_enabled: true,
    kb_selection_mode: 'selected' as const, knowledge_bases: ['private-kb'], knowledge_ids: ['private-doc'],
    mcp_selection_mode: 'selected' as const, mcp_services: ['private-mcp'],
    skills_selection_mode: 'selected' as const, selected_skills: ['private-skill'],
    question_suggestions: { follow_ups: { model_id: 'private-followup', enabled: true } },
  })
  const result: any = systemAgentConfig(original as any)
  for (const field of ['model_id', 'sandbox_config_id', 'temperature', 'knowledge_ids']) assert.equal(result[field], undefined)
  assert.deepEqual(result.knowledge_bases, [])
  assert.deepEqual(result.mcp_services, [])
  assert.deepEqual(result.selected_skills, [])
  assert.equal(result.kb_selection_mode, 'all')
  assert.equal(result.mcp_selection_mode, 'none')
  assert.equal(result.skills_selection_mode, 'none')
  assert.equal(result.system_prompt, original.system_prompt)
  assert.deepEqual(result.allowed_tools, ['web_search'])
  assert.equal(result.question_suggestions.follow_ups.model_id, undefined)
  assert.equal(original.model_id, 'private-model')
  assert.deepEqual(original.knowledge_bases, ['private-kb'])
})

test('global all and none scopes keep caller-scoped capabilities', () => {
  const config = systemAgentConfig({ kb_selection_mode: 'none', mcp_selection_mode: 'all', skills_selection_mode: 'all' })
  assert.equal(config.kb_selection_mode, 'none')
  assert.equal(config.mcp_selection_mode, 'all')
  assert.equal(config.skills_selection_mode, 'all')
})

test('workspace Owners cannot manage global built-ins while personal agent permissions remain', () => {
  const owner = { isSystemAdmin: false, user: { id: 'owner' }, hasRole: () => true }
  assert.equal(canManageAgentResource(agent(), owner), false)
  assert.equal(canManageAgentResource(agent({ is_builtin: false }), owner), true)
  assert.equal(canManageAgentResource(agent(), { ...owner, isSystemAdmin: true, hasRole: () => false }), true)
  assert.equal(canManageAgentResource(agent(), owner, false), true, 'flag off preserves workspace builtin editor')
  const contributor = { ...owner, hasRole: (r: string) => r === 'contributor' }
  assert.equal(canManageAgentResource(agent({ is_builtin: false, created_by: 'owner' }), contributor), true)
  assert.equal(canManageAgentResource(agent({ is_builtin: false, created_by: 'other' }), contributor), false)
})

test('missing or removed built-ins never become synthetic agents and renamed data remains authoritative', () => {
  const current = agent({ name: 'Administrator rename' })
  assert.equal(resolveSelectedAgent([current], current.id, null, []), current)
  assert.equal(resolveSelectedAgent([], current.id, null, []), undefined)
  assert.equal(resolveSelectedAgent([], 'builtin-quick-answer', null, []), undefined)
  assert.equal(resolveSelectedAgent([agent({ is_internal: true })], current.id, null, []), undefined)
})

test('a missing shared source cannot resolve to a same-ID local or global agent', () => {
  const current = agent()
  assert.equal(resolveSelectedAgent([current], current.id, '2', []), undefined)
  assert.equal(resolveSelectedAgent([current], current.id, '2', [{ source_tenant_id: 3, agent: current }]), undefined)
  assert.equal(resolveSelectedAgent([], current.id, '2', [{ source_tenant_id: 2, agent: current }]), current)
})

test('system defaults cover administrator global built-ins without changing administrator personal models', () => {
  assert.equal(agentUsesSystemModels(agent(), true, false), true)
  assert.equal(agentUsesSystemModels(agent({ is_builtin: false }), true, false), false)
  assert.equal(agentUsesSystemModels(agent(), false, false), false)
  assert.equal(agentUsesSystemModels(agent({ is_builtin: false }), true, true), true)
})
