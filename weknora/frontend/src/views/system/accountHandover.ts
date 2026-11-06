import type { HandoverResource } from '@/api/spaces/handover'

export const resourceKey = (r: HandoverResource) => JSON.stringify([r.kind, r.source_tenant_id, r.resource_id])
export function groupHandoverResources(rows: HandoverResource[]) {
  const grouped = new Map<string, { key: string; resource: HandoverResource; spaces: { value: string; label: string }[] }>()
  for (const row of rows) {
    const key = resourceKey(row)
    const item = grouped.get(key) || { key, resource: row, spaces: [] }
    if (!item.spaces.some(s => s.value === row.space_id)) item.spaces.push({ value: row.space_id, label: row.space_name })
    grouped.set(key, item)
  }
  return [...grouped.values()].sort((a, b) => Number(a.resource.kind === 'agent') - Number(b.resource.kind === 'agent'))
}
