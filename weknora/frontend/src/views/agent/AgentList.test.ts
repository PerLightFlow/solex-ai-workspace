import assert from 'node:assert/strict'
import test, { type TestContext } from 'node:test'
import * as vue from 'vue'
import * as sorting from '../../utils/resourceSorting.ts'
import * as systemAgents from '../../config/systemAgents.ts'
import { change, fixture, flush } from '../../test-utils/vueFixture.ts'

function setup(t: TestContext, systemAdmin = false, managedRuntime = true) {
  const f = fixture(t)
  const fetchAgentsForList = t.mock.fn(async (_params: unknown, _force: boolean) => ({
    data: [{ id: 'agent', name: 'Updated agent' }], disabled_own_agent_ids: [],
  }))
  const confirmDelete = t.mock.fn((_options: any) => {})
  const deleteSystemAgent = t.mock.fn(async (_id: string) => ({ success: true }))
  const deleteAgent = t.mock.fn(async (_id: string) => ({ success: true }))
  const getSystemAgent = t.mock.fn(async (id: string) => ({ data: { id, name: 'Current global', is_builtin: true, config: {} } }))
  const page = f.component(new URL('./AgentList.vue', import.meta.url), {
    'vue-router': { useRoute: () => ({ query: {} }), useRouter: () => ({}) },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    'tdesign-vue-next': { MessagePlugin: { success() {}, error() {} } },
    '@/stores/auth': { useAuthStore: () => ({ isSystemAdmin: systemAdmin, hasRole: () => true }) },
    '@/stores/deploymentCapabilities': { useDeploymentCapabilitiesStore: () => ({ managedRuntime }) },
    '@/config/systemAgents': systemAgents,
    '@/stores/ui': { useUIStore: () => ({}) },
    '@/stores/organization': { useOrganizationStore: () => ({
      sharedAgents: [], async fetchOrganizations() {}, async fetchSharedAgents() {},
    }) },
    '@/stores/chatResources': { useChatResourcesStore: () => ({ fetchAgentsForList }) },
    '@/stores/settings': { useSettingsStore: () => ({}) },
    '@/stores/menu': { useMenuStore: () => ({}) },
    '@/components/settings/useConfirmDelete': { useConfirmDelete: () => confirmDelete },
    '@/composables/useTenantModelReadiness': { useTenantModelReadiness: () => ({ loaded: vue.ref(true), isReadyForAgent: vue.ref(true) }) },
    '@/composables/useListUrlState': { useListUrlState: () => ({ scope: vue.ref('all'), creator: vue.ref('all'), query: vue.ref('') }) },
    '@/composables/useResourcePins': { useResourcePins: () => ({ favorites: vue.ref([]), recents: vue.ref([]) }) },
    '@/composables/useAssistantPageContext': { useAssistantPageContext() {} },
    '@/stores/assistant': { useAssistantStore: () => ({ pageAccess: true }) },
    '@/api/agent': { deleteSystemAgent, deleteAgent, getSystemAgent }, '@/api/chat/index': {}, '@/api/organization': {},
    '@/config/contextualGuides': { markContextualGuideDone() {} }, '@/config/settingsRoute': {},
    '@/utils/resourceListSearch': {}, '@/utils/card-list-badge': {}, '@/utils/resourceSorting': sorting,
  })
  return { ...f, ...page, fetchAgentsForList, confirmDelete, deleteSystemAgent, deleteAgent, getSystemAgent }
}

test('refreshes the mounted agent list once per burst, ignoring other areas and unmounted pages', async t => {
  const f = setup(t)
  await flush()
  assert.equal(f.fetchAgentsForList.mock.callCount(), 1)
  f.fetchAgentsForList.mock.resetCalls()
  f.emit(change('knowledge.update'))
  t.mock.timers.tick(300)
  assert.equal(f.fetchAgentsForList.mock.callCount(), 0)
  f.emit(change('agent.create'))
  f.emit(change('agent.delete'))
  t.mock.timers.tick(300)
  await flush()
  assert.equal(f.fetchAgentsForList.mock.callCount(), 1)
  assert.equal(f.fetchAgentsForList.mock.calls[0].arguments[1], true)
  assert.equal(f.vm.agents[0].name, 'Updated agent')
  f.close()
  f.emit(change('agent.update'))
  t.mock.timers.tick(300)
  assert.equal(f.fetchAgentsForList.mock.callCount(), 1)
})

test('the assistant create entry opens its own dialog instead of the editor', async t => {
  const f = setup(t)
  await flush()
  assert.equal(f.vm.assistantCreateVisible, false)
  f.vm.assistantCreateVisible = true
  assert.equal(f.vm.assistantCreateVisible, true)
  assert.equal(f.vm.editorVisible, false)
})

test('the original create entry opens the configuration form directly', async t => {
  const f = setup(t)
  await flush()
  f.vm.editingAgent = { id: 'previous-agent' }
  f.vm.editorMode = 'edit'
  f.vm.editorInitialSection = 'knowledge'
  f.vm.handleCreateAgent()
  assert.equal(f.vm.editorVisible, true)
  assert.equal(f.vm.editorMode, 'create')
  assert.equal(f.vm.editorInitialSection, 'basic')
  assert.equal(f.vm.editingAgent, null)
})


test('ordinary workspace Owner keeps personal management but cannot add or remove globals', async t => {
  const f = setup(t)
  await flush()
  assert.equal(f.vm.canManageAgent({ is_builtin: true }), false)
  assert.equal(f.vm.canManageAgent({ is_builtin: false }), true)
  f.vm.handleCreateSystemAgent()
  assert.equal(f.vm.editorVisible, false)
  f.vm.handleDelete({ id: 'builtin-global', is_builtin: true })
  assert.equal(f.confirmDelete.mock.callCount(), 0)
})

test('system administrator edits authoritative global details and confirms all-user removal', async t => {
  const f = setup(t, true)
  await flush()
  f.vm.handleCreateSystemAgent()
  assert.equal(f.vm.systemEditor, true)
  assert.equal(f.vm.editorMode, 'create')
  await f.vm.handleEdit({ id: 'builtin-global', is_builtin: true, name: 'stale' })
  assert.equal(f.vm.editingAgent.name, 'Current global')
  f.vm.handleDelete({ id: 'builtin-global', is_builtin: true, name: 'Current global' })
  assert.equal(f.deleteSystemAgent.mock.callCount(), 0)
  const confirmation = f.confirmDelete.mock.calls[0].arguments[0]
  assert.equal(confirmation.body, 'systemAgents.removeConfirm')
  await confirmation.onConfirm()
  assert.equal(f.deleteSystemAgent.mock.calls[0].arguments[0], 'builtin-global')
  assert.equal(f.deleteAgent.mock.callCount(), 0)
  assert.equal(f.fetchAgentsForList.mock.calls.at(-1)?.arguments[1], true)
})

test('disabled managed deployment retains old builtin editing without global management', async t => {
  const f = setup(t, true, false)
  await flush()
  f.vm.handleCreateSystemAgent()
  assert.equal(f.vm.editorVisible, false)
  const legacy = { id: 'builtin-quick-answer', is_builtin: true, name: 'Legacy' }
  assert.equal(f.vm.canManageAgent(legacy), true)
  assert.equal(f.vm.canDeleteAgent(legacy), false)
  await f.vm.handleEdit(legacy)
  assert.equal(f.vm.systemEditor, false)
  assert.equal(f.getSystemAgent.mock.callCount(), 0)
})
