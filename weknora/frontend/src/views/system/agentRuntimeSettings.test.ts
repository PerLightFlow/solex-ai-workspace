import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { agentRuntimeSettingBounds, isValidAgentRuntimeSettingValue } from './agentRuntimeSettings'

const source = readFileSync(new URL('./SystemSettings.vue', import.meta.url), 'utf8')
const persist = source.slice(source.indexOf('async function persistSetting('), source.indexOf('\nonMounted('))
assert.ok(persist.startsWith('async function persistSetting('))

function harness(key: string, original: number, edited: unknown, fail = false) {
  const writes: unknown[] = []
  const errors: string[] = []
  const successes: string[] = []
  const item = { key, value: original }
  const context: any = {
    settings: { value: [item] }, editValues: { [key]: edited },
    savedKey: { value: null }, savingKey: { value: null }, saveAnnouncement: { value: '' },
    isValidAgentRuntimeSettingValue, agentRuntimeSettingBounds,
    keyLabel: (name: string) => name,
    t: (name: string, params?: Record<string, unknown>) => params ? `${name}:${params.min}-${params.max}` : name,
    markSettingSaved: () => successes.push('saved'),
    MessagePlugin: { error: (message: string) => errors.push(message), success: (message: string) => successes.push(message) },
    refreshSandboxDockerCapability: async () => {},
    updateSystemSetting: async (name: string, value: unknown) => {
      writes.push({ key: name, value })
      if (fail) throw new Error('Server rejected update')
      return { key: name, value }
    },
  }
  const code = ts.transpileModule(persist + '\nglobalThis.save = persistSetting', {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText
  runInNewContext(code, context)
  return { context, writes, errors, successes, save: () => context.save(item) }
}

test('agent execution limits accept bounded integers and reject invalid edits', () => {
  for (const [key, maximum] of [['agent.max_iterations', 200], ['agent.collaboration_wait_timeout_seconds', 60]] as const) {
    for (const value of [1, 30, maximum]) assert.equal(isValidAgentRuntimeSettingValue(key, value), true)
    for (const value of [0, -1, maximum + 1, 1.5, null, undefined, '', '30', NaN, Infinity]) {
      assert.equal(isValidAgentRuntimeSettingValue(key, value), false, `${key}: ${String(value)}`)
    }
  }
  // Existing setting types keep their established server-side validation.
  assert.equal(isValidAgentRuntimeSettingValue('model.max_concurrency', -1), true)
  assert.equal(agentRuntimeSettingBounds('toString'), undefined)
})

test('saving a valid global execution value sends it once and shows canonical success', async () => {
  for (const [key, value] of [['agent.max_iterations', 200], ['agent.collaboration_wait_timeout_seconds', 60]] as const) {
    const h = harness(key, 30, value)
    await h.save()
    assert.deepEqual(h.writes, [{ key, value }])
    assert.deepEqual(h.errors, [])
    assert.deepEqual(h.successes, ['saved', 'system.globalSettings.messages.saveSuccess'])
    assert.equal(h.context.settings.value[0].value, value)
    assert.equal(h.context.editValues[key], value)
    assert.equal(h.context.savingKey.value, null)
  }
})

test('invalid values do not send a request or look saved, and restore the prior value', async () => {
  for (const [key, maximum] of [['agent.max_iterations', 200], ['agent.collaboration_wait_timeout_seconds', 60]] as const) {
    for (const value of [0, maximum + 1, 2.5, null, '30']) {
      const h = harness(key, 30, value)
      await h.save()
      assert.deepEqual(h.writes, [])
      assert.deepEqual(h.successes, [])
      assert.deepEqual(h.errors, [`system.globalSettings.messages.integerRange:1-${maximum}`])
      assert.equal(h.context.editValues[key], 30)
      assert.equal(h.context.settings.value[0].value, 30)
      assert.equal(h.context.savingKey.value, null)
    }
  }
})

test('failed server writes restore the canonical value and retain error feedback', async () => {
  const h = harness('agent.max_iterations', 50, 70, true)
  await h.save()
  assert.equal(h.writes.length, 1)
  assert.deepEqual(h.errors, ['Server rejected update'])
  assert.deepEqual(h.successes, [])
  assert.equal(h.context.editValues['agent.max_iterations'], 50)
  assert.equal(h.context.saveAnnouncement.value, 'Server rejected update')
  assert.equal(h.context.savingKey.value, null)
})
