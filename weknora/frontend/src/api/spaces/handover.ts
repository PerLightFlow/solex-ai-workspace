import { get, post } from '@/utils/request'

export interface HandoverSpace { id: string; name: string; owner_id: string }
export interface HandoverResource {
  kind: 'knowledge_base' | 'agent'; resource_id: string; source_tenant_id: number
  name: string; owner_user_id: string; space_id: string; space_name: string
}
export interface AccountHandover {
  user_id: string; username: string; is_active: boolean
  spaces: HandoverSpace[]; resources: HandoverResource[]
}
export const getAccountHandover = (id: string) => get<{ data: AccountHandover }>(
  `/api/v1/spaces/admin/users/${encodeURIComponent(id)}/handover`).then(r => r.data)
export const handoverSpace = (space: string, user_id: string, owner_user_id: string) => post(
  `/api/v1/spaces/admin/handovers/${encodeURIComponent(space)}`, { user_id, owner_user_id })
export const handoverResource = (resource: HandoverResource, space_id: string, owner_user_id: string) => post(
  '/api/v1/spaces/admin/resource-handovers', {
    kind: resource.kind, resource_id: resource.resource_id,
    source_tenant_id: resource.source_tenant_id, space_id, owner_user_id,
  })
