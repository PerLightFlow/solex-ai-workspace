import { get, post, put, del } from '@/utils/request'

export type SpaceRole = 'viewer' | 'editor' | 'admin' | 'owner'
export const roleLabel: Record<SpaceRole, string> = { viewer: '使用者', editor: '编辑者', admin: '管理员', owner: '所有者' }
export const roleLevel: Record<SpaceRole, number> = { viewer: 1, editor: 2, admin: 3, owner: 4 }
export interface Space { archived?: boolean; id: string; name: string; description: string; owner_id: string; my_role: SpaceRole; member_count: number }
export interface Member { user_id: string; username: string; role: SpaceRole; joined_at: string; active: boolean }
export interface PermissionSource { space_id: string; space_name: string; role: SpaceRole; allow_edit: boolean; allow_download: boolean }
export interface Resource {
  kind: 'knowledge_base' | 'agent'; id: string; name: string; source_tenant_id: number; owned: boolean
  access: { read: boolean; edit: boolean; download: boolean; manage: boolean; sources: PermissionSource[] }
  grant?: { allow_edit: boolean; allow_download: boolean }
}
export interface Invitation { email?: string; space_name?: string; id: string; space_id: string; expires_at: string }
export interface Event { id: number; actor_id: string; action: string; subject: string; created_at: string }
type Response<T> = { success: boolean; data: T; message?: string }
const base = '/api/v1/spaces'
const segment = encodeURIComponent
export const listSpaces = () => get<Response<Space[]>>(base).then(r => r.data)
export const createSpace = (name: string, description: string) => post(base, { name, description })
export const listMembers = (id: string) => get<Response<Member[]>>(`${base}/${segment(id)}/members`).then(r => r.data)
export const listResources = (id: string) => get<Response<Resource[]>>(`${base}/${segment(id)}/resources`).then(r => r.data)
export const listEvents = (id: string) => get<Response<Event[]>>(`${base}/${segment(id)}/events`).then(r => r.data)
export const listInvitations = () => get<Response<Invitation[]>>(`${base}/invitations`).then(r => r.data)
export const acceptInvitation = (id: string) => post(`${base}/invitations/${segment(id)}/accept`)
export const inviteMember = (id: string, email: string) => post(`${base}/${segment(id)}/invitations`, { email })
export const changeRole = (id: string, user: string, role: SpaceRole) => put(`${base}/${segment(id)}/members/${segment(user)}`, { role })
export const removeMember = (id: string, user: string) => del(`${base}/${segment(id)}/members/${segment(user)}`)
export const transferSpace = (id: string, user_id: string) => post(`${base}/${segment(id)}/transfer`, { user_id })
export const deleteSpace = (id: string) => del(`${base}/${segment(id)}`)
export const shareResource = (id: string, resource: Pick<Resource, 'kind' | 'id' | 'source_tenant_id'>, allow_edit: boolean, allow_download: boolean) => post(`${base}/${segment(id)}/resources`, { ...resource, allow_edit, allow_download })
export const unshareResource = (id: string, resource: Pick<Resource, 'kind' | 'id' | 'source_tenant_id'>) => del(`${base}/${segment(id)}/resources`, resource)
export const transferResource = (id: string, resource: Pick<Resource, 'kind' | 'id' | 'source_tenant_id'>) => post(`${base}/${segment(id)}/resources/transfer`, resource)
