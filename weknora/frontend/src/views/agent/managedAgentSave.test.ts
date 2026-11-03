import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { managedAgentConfig } from '../../config/managedRuntime'
import { systemAgentConfig } from '../../config/systemAgents'

const source = readFileSync(new URL('./AgentEditorModal.vue', import.meta.url), 'utf8')
const save = source.match(/const handleSave = async \(\) => \{[\s\S]*?\n\};\n<\/script>/)?.[0].replace('\n</script>', '')
assert.ok(save)

function harness(managed: boolean, system = false, admin = false) {
  const errors: string[] = []
  const saved: any[] = []
  const globalSaved: any[] = []
  let closed = false
  const context: any = {
    props: { readOnly: false, systemBuiltin: system }, authStore: { isSystemAdmin: admin }, systemAgentConfig,
    handleClose() { closed = true },
    createSystemAgent: async (payload: any) => { globalSaved.push(payload); return { data: { id: 'builtin-global', config: payload.config } } },
    managedRuntime: { value: managed }, usesGlobalAgentBudget: { value: managed }, isBuiltinAgent: { value: false }, isAgentMode: { value: true },
    formData: { value: { name: 'Personal hardware agent', config: {
      system_prompt: 'Answer hardware questions', model_id: '',
      question_suggestions: { starters: { items: ['Which chip?'] } },
      allowed_tools: ['web_search'], web_search_enabled: true,
    } } },
    currentSection: { value: 'basic' }, editorMode: { value: 'create' }, hostOnly: { value: false },
    saving: { value: false }, savedAgent: { value: null }, storedToolNames: { value: [] }, keepAllowedToolsUnset: { value: false },
    promptTemplates: { value: null }, MessagePlugin: { error: (x: string) => errors.push(x), success() {} },
    t: (key: string) => key, hasPlaceholder: () => true, pruneSelectedSkills() {},
    allowedToolsToSave: () => ['web_search'], serializeAgentPrompts: (c: any) => c, managedAgentConfig,
    createAgent: async (payload: any) => { saved.push(payload); return { data: { id: 'agent-1', config: payload.config } } },
    normalizeLegacyToolNames: (x: unknown) => x, modalShell: { markClean() {} },
    markContextualGuideDone() {}, loadAgentIntegrationCounts() {}, notifyAllowedToolsWarnings() {}, emit() {},
  }
  const code = ts.transpileModule(save + '\nglobalThis.save = handleSave', { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
  runInNewContext(code, context)
  return { context, errors, saved, globalSaved, get closed() { return closed } }
}

test('ordinary managed user can save a personal agent without choosing a model', async () => {
  const h = harness(true)
  await h.context.save()
  assert.deepEqual(h.errors, [])
  assert.equal(h.saved.length, 1)
  assert.equal(h.saved[0].config.model_id, undefined)
  assert.equal(h.saved[0].config.system_prompt, 'Answer hardware questions')
  assert.equal(h.context.saving.value, false)
})

test('unmanaged editor still requires a model and does not submit an incomplete agent', async () => {
  const h = harness(false)
  await h.context.save()
  assert.deepEqual(h.errors, ['agent.editor.modelRequired'])
  assert.equal(h.saved.length, 0)
})

test('managed deployment administrators cannot replay an old per-agent execution budget', async () => {
  const h = harness(false, false, true)
  h.context.usesGlobalAgentBudget.value = true
  Object.assign(h.context.formData.value.config, { model_id: 'admin-model', max_iterations: -1 })
  await h.context.save()
  assert.deepEqual(h.errors, [])
  assert.equal(h.saved.length, 1)
  assert.equal(h.saved[0].config.max_iterations, undefined)
  assert.equal(h.saved[0].config.model_id, 'admin-model')
  assert.equal(h.context.formData.value.config.max_iterations, -1)
})

test('unmanaged deployments retain the selected per-agent execution budget', async () => {
  const h = harness(false, false, true)
  Object.assign(h.context.formData.value.config, { model_id: 'admin-model', max_iterations: 20 })
  await h.context.save()
  assert.deepEqual(h.errors, [])
  assert.equal(h.saved[0].config.max_iterations, 20)
})


test('global editor saves to the system API using caller-scoped defaults and closes', async () => {
  const h = harness(true, true, true)
  Object.assign(h.context.formData.value.config, { model_id: 'private', knowledge_bases: ['private-kb'], kb_selection_mode: 'selected' })
  await h.context.save()
  assert.deepEqual(h.errors, [])
  assert.equal(h.saved.length, 0)
  assert.equal(h.globalSaved.length, 1)
  assert.equal(h.globalSaved[0].config.model_id, undefined)
  assert.deepEqual(h.globalSaved[0].config.knowledge_bases, [])
  assert.equal(h.closed, true)
})

test('non-system administrators cannot submit a forged global editor and readonly cannot save', async () => {
  const denied = harness(true, true, false)
  await denied.context.save()
  assert.equal(denied.globalSaved.length, 0)
  const readonly = harness(true)
  readonly.context.props.readOnly = true
  await readonly.context.save()
  assert.equal(readonly.saved.length, 0)
})

test('global save waits for all-user confirmation while personal save does not', async () => {
  const request = source.slice(source.indexOf('const requestSave ='), source.indexOf('const handleSave ='))
  for (const systemBuiltin of [false, true]) {
    let saves = 0
    let confirmation: any
    const run = runInNewContext(ts.transpile(request + '\nrequestSave'), {
      props: { systemBuiltin, readOnly: false }, formData: { value: { name: 'Global' } },
      t: (key: string) => key, handleSave: async () => { saves++ },
      DialogPlugin: { confirm: (options: any) => { confirmation = options; return { hide() {} } } },
    })
    run()
    assert.equal(saves, systemBuiltin ? 0 : 1)
    if (systemBuiltin) {
      assert.equal(confirmation.body, 'systemAgents.saveConfirm')
      await confirmation.onConfirm()
      assert.equal(saves, 1)
    }
  }
})
