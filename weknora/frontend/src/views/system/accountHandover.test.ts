import { test } from 'node:test'
import assert from 'node:assert/strict'
import { groupHandoverResources } from './accountHandover'

test('handover groups an original shared to multiple spaces without merging distinct tenant or resource kinds', () => {
  const base = { kind: 'knowledge_base' as const, resource_id: 'id', source_tenant_id: 1,
    name: '资料', owner_user_id: 'owner', space_id: 's1', space_name: '项目组' }
  const result = groupHandoverResources([
    { ...base, kind: 'agent' }, base, { ...base, space_id: 's2', space_name: '合作组' }, base,
    { ...base, source_tenant_id: 2 },
  ])
  assert.equal(result.length, 3)
  assert.deepEqual(result[0].spaces.map(s => s.value), ['s1', 's2'])
  assert.equal(result[1].resource.source_tenant_id, 2)
  assert.equal(result[2].resource.kind, 'agent')
})
