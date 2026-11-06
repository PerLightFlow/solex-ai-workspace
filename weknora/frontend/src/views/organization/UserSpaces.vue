<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { createSessions } from '@/api/chat'
import { get, post, put, del } from '@/utils/request'
import * as api from '@/api/spaces'
import AgentEditorModal from '@/views/agent/AgentEditorModal.vue'
import KnowledgeBaseEditorModal from '@/views/knowledge/KnowledgeBaseEditorModal.vue'
import SpaceInviteDialog from './SpaceInviteDialog.vue'
import type { CustomAgent } from '@/api/agent'
const kbCreateVisible = ref(false), agentEditorVisible = ref(false)
const editingAgent = ref<CustomAgent | null>(null)
const pendingInvites = ref<api.Invitation[]>([])
const recycleVisible = ref(false), recycledResources = ref<any[]>([])
async function openRecycle(){recycledResources.value=(await get("/api/v1/spaces/recycled-resources")).data;recycleVisible.value=true}

const router = useRouter(), auth = useAuthStore(), settings = useSettingsStore()
const spaces = ref<api.Space[]>([]), selected = ref<api.Space | null>(null)
const members = ref<api.Member[]>([]), resources = ref<api.Resource[]>([]), events = ref<api.Event[]>([])
const invitations = ref<api.Invitation[]>([]), busy = ref(false), error = ref('')
const tab = ref('resources'), createVisible = ref(false), shareVisible = ref(false), inviteVisible = ref(false)
const name = ref(''), description = ref(''), candidates = ref<api.Resource[]>([]), choice = ref('')
const allowEdit = ref(false), allowDownload = ref(false)
const manage = computed(() => selected.value && api.roleLevel[selected.value.my_role] >= 3)
const publish = computed(() => selected.value && !selected.value.archived && api.roleLevel[selected.value.my_role] >= 2)
const self = computed(() => auth.user?.id || '')
const resource = computed(() => candidates.value.find(r => `${r.kind}:${r.id}` === choice.value))
const actionLabel: Record<string, string> = { 'space.created': '创建空间', 'member.invited': '邀请成员', 'member.joined': '加入空间', 'member.role_changed': '调整成员权限', 'space.transferred': '移交空间', 'resource.created': '创建空间资源', 'resource.shared': '修改共享授权', 'resource.unshared': '撤回共享', 'resource.transferred': '移交资源', 'space.archived': '归档空间', 'space.reopened': '恢复空间', 'space.admin_handover': '管理员移交空间', 'resource.admin_handover': '管理员移交共享资源' }
const when = (value: string) => new Date(value).toLocaleString()

async function run(fn: () => Promise<unknown>) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await fn() } catch (e: any) { error.value = e?.response?.data?.message || e?.message || '操作失败，请重试'; MessagePlugin.error(error.value) }
  finally { busy.value = false }
}
async function refresh() {
  const [items, invites] = await Promise.all([api.listSpaces(), api.listInvitations()])
  spaces.value = items; invitations.value = invites
  if (selected.value) {
    selected.value = items.find(s => s.id === selected.value?.id) || null
    if (selected.value) await detail(selected.value)
  }
}
async function detail(space: api.Space) {
  selected.value = space
  const [m, r, e] = await Promise.all([api.listMembers(space.id), api.listResources(space.id), api.listEvents(space.id)])
  members.value = m; resources.value = r; events.value = e
 pendingInvites.value = manage.value ? (await get(`/api/v1/spaces/${space.id}/invitations`)).data : []
}
function confirm(message: string, fn: () => Promise<unknown>) {
  const dialog = DialogPlugin.confirm({ header: '确认操作', body: message, confirmBtn: '确认', cancelBtn: '取消', onConfirm: () => { dialog.hide(); void run(fn) } })
}
async function create() {
  await api.createSpace(name.value.trim(), description.value.trim()); createVisible.value = false; name.value = ''; description.value = ''; await refresh()
}
async function openShare() {
  const [kb, agents] = await Promise.all([get('/api/v1/knowledge-bases'), get('/api/v1/agents')])
  candidates.value = [
    ...(Array.isArray(kb.data) ? kb.data : []).map((r: any) => ({ ...r, kind: 'knowledge_base' })),
    ...(Array.isArray(agents.data) ? agents.data : []).filter((r: any) => !r.is_builtin).map((r: any) => ({ ...r, kind: 'agent' })),
  ].map((r: any) => ({ ...r, source_tenant_id: r.tenant_id }))
  choice.value = ''; allowEdit.value = false; allowDownload.value = false; shareVisible.value = true
}
async function share() {
  if (!resource.value || !selected.value) return
  await api.shareResource(selected.value.id, resource.value, allowEdit.value, allowDownload.value)
  shareVisible.value = false; await refresh()
}
async function useResource(r: api.Resource) {
  if (r.kind === 'knowledge_base') { await router.push(`/platform/knowledge-bases/${r.id}`); return }
  settings.selectAgent(r.id, String(r.source_tenant_id))
  const response = await createSessions({})
  if (!response.data?.id) throw new Error('无法创建对话')
  await router.push({ path: `/platform/chat/${response.data.id}`, query: { agent_id: r.id, source_tenant_id: String(r.source_tenant_id) } })
}
async function editAgent(r: api.Resource) {
  editingAgent.value = (await get(`/api/v1/agents/${r.id}`, { headers: { 'X-Source-Tenant-ID': r.source_tenant_id } })).data
  agentEditorVisible.value = true
}
onMounted(() => run(refresh))
</script>

<template>
  <main class="spaces-page" :aria-busy="busy">
    <header><div><button v-if="selected" class="back" @click="selected = null">← 所有共享空间</button><h1>{{ selected?.name || '共享空间' }}</h1><p>{{ selected?.description || '按用户邀请成员，共享指定的知识库与智能体。个人空间始终仅自己使用。' }}</p></div><div v-if="!selected" class="actions"><t-button @click="createVisible = true">创建共享空间</t-button><t-button variant="outline" @click="run(openRecycle)">资源回收站</t-button></div><div v-else class="actions"><t-tag>{{ api.roleLabel[selected.my_role] }}</t-tag><t-tag v-if="selected.archived" theme="warning">已归档</t-tag><t-button v-if="manage && !selected.archived" variant="outline" @click="inviteVisible = true">邀请用户</t-button><t-button v-if="publish" @click="run(openShare)">共享我的资源</t-button><t-button v-if="publish" variant="outline" @click="kbCreateVisible = true">新建空间知识库</t-button><t-button v-if="publish" variant="outline" @click="editingAgent = null; agentEditorVisible = true">新建空间智能体</t-button></div></header>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <section v-if="!selected && invitations.length" class="invitations"><h2>待接受的邀请</h2><div v-for="i in invitations" :key="i.id">{{ i.space_name || '共享空间邀请' }} · {{ when(i.expires_at) }} 前有效<t-button size="small" @click="run(async () => { await api.acceptInvitation(i.id); await refresh() })">以使用者身份加入</t-button><t-button variant="text" @click="run(async () => { await del(`/api/v1/spaces/invitations/${i.id}`); await refresh() })">拒绝</t-button></div></section>
    <section v-if="!selected" class="space-grid"><button v-for="s in spaces" :key="s.id" class="space-card" @click="run(() => detail(s))"><strong>{{ s.name }}</strong><p>{{ s.description || '共享知识与协作' }}</p><span>{{ s.member_count }} 位成员 · {{ api.roleLabel[s.my_role] }}{{ s.archived ? ' · 已归档' : '' }}</span></button><p v-if="!spaces.length && !busy" class="empty">还没有共享空间。创建一个空间，或接受其他成员的邀请。</p></section>
    <template v-else>
      <nav class="tabs"><button v-for="entry in [['resources','资源'],['members','成员'],['events','动态'],['settings','设置']]" :key="entry[0]" :class="{ active: tab === entry[0] }" @click="tab = entry[0] || 'resources'">{{ entry[1] }}</button></nav>
      <section v-if="tab === 'resources'" class="resource-grid">
        <article v-for="r in resources" :key="`${r.kind}:${r.source_tenant_id}:${r.id}`" class="resource-card">
          <t-tag>{{ r.kind === 'agent' ? '智能体' : '知识库' }}</t-tag>
          <h2>{{ r.name }}</h2>
          <p>{{ r.owned ? `归属：${selected.name}` : '共享原件' }} · {{ r.access.edit ? '可编辑' : '只读使用' }} · {{ r.access.download ? '可下载' : '不开放原文件下载' }}</p>
          <details v-if="r.access.sources.length">
            <summary>权限来源</summary>
            <p v-for="source in r.access.sources" :key="source.space_id">
              你在{{ source.space_name || selected.name }}的角色是「{{ api.roleLabel[source.role] }}」
              <span class="source-capabilities">{{ source.allow_edit ? '可编辑' : '只读使用' }} · {{ source.allow_download ? '可下载' : '不开放原文件下载' }}</span>
            </p>
          </details>
          <div class="actions">
            <t-button @click="run(() => useResource(r))">{{ r.kind === 'agent' ? '使用智能体' : '打开知识库' }}</t-button>
            <t-button v-if="r.kind === 'agent' && r.access.edit" variant="text" @click="run(() => editAgent(r))">编辑智能体</t-button>
            <t-button v-if="r.access.manage && manage && !selected.archived && !r.owned" variant="text" @click="confirm('将资源所有权交给此共享空间，原文件与链接保持不变。', async () => { await api.transferResource(selected!.id, r); await refresh() })">移交空间</t-button>
            <t-button v-if="manage && !r.owned" variant="text" theme="danger" @click="confirm('撤回此空间的共享关系，原资源保留。用户仍可能通过其他空间获得授权。', async () => { await api.unshareResource(selected!.id, r); await refresh() })">撤回共享</t-button>
          </div>
        </article>
        <p v-if="!resources.length" class="empty">尚未发布资源。共享知识库默认只读；需要协作时明确开启“允许修改原件”。</p>
      </section>
      <section v-if="tab === 'members'"><div v-for="i in pendingInvites" :key="i.id" class="member"><div>{{ i.email }}<small>待接受 · {{ when(i.expires_at) }} 到期</small></div><t-button variant="text" @click="run(async () => { await del(`/api/v1/spaces/invitations/${i.id}`); await refresh() })">撤销邀请</t-button></div><div v-for="m in members" :key="m.user_id" class="member"><div><strong>{{ m.username }}</strong><small>{{ m.active ? '有效账号' : '账号已停用' }} · {{ when(m.joined_at) }} 加入</small></div><select v-if="manage && m.role !== 'owner' && (selected.my_role === 'owner' || m.role !== 'admin')" :value="m.role" :aria-label="`${m.username}的角色`" @change="run(async () => { await api.changeRole(selected!.id, m.user_id, ($event.target as HTMLSelectElement).value as api.SpaceRole); await refresh() })"><option value="viewer">使用者</option><option value="editor">编辑者</option><option v-if="selected.my_role === 'owner'" value="admin">管理员</option></select><span v-else>{{ api.roleLabel[m.role] }}</span><t-button v-if="m.role !== 'owner' && (selected.my_role === 'owner' || (manage && m.role !== 'admin') || m.user_id === self)" variant="text" theme="danger" @click="confirm('移除后，此用户不能再通过该空间访问资源。', async () => { await api.removeMember(selected!.id, m.user_id); await refresh() })">{{ m.user_id === self ? '退出' : '移除' }}</t-button><t-button v-if="selected.my_role === 'owner' && m.user_id !== self" variant="text" @click="confirm(`将空间所有权转交给 ${m.username}，你将保留管理员角色。`, async () => { await api.transferSpace(selected!.id, m.user_id); await refresh() })">转交所有权</t-button></div></section>
      <section v-if="tab === 'events'"><div v-for="event in events" :key="event.id" class="event"><time>{{ when(event.created_at) }}</time><strong>{{ members.find(m => m.user_id === event.actor_id)?.username || '已退出成员' }}</strong><span>{{ actionLabel[event.action] || event.action }}</span><span>{{ event.subject }}</span></div><p v-if="!events.length">暂无动态</p></section>
      <section v-if="tab === 'settings'" class="settings"><h2>空间管理</h2><p>新邀请默认以使用者身份加入；个人资源的修改和下载能力由资源所有者授权。</p><p>空间所有者退出前须转交管理权；解散前须处理空间自有资源。</p><t-button v-if="selected.my_role === 'owner'" variant="outline" @click="confirm(selected.archived ? '恢复后成员可按原权限修改资料。' : '归档后保留阅读，停止新增和修改协作内容。', async () => { await put(`/api/v1/spaces/${selected!.id}/archive`, { archived: !selected!.archived }); await refresh() })">{{ selected.archived ? '恢复空间' : '归档空间' }}</t-button><t-button v-if="selected.my_role === 'owner'" theme="danger" variant="outline" @click="confirm('解散空间将解除共享关系，但不会删除个人原件。', async () => { await api.deleteSpace(selected!.id); selected = null; await refresh() })">解散共享空间</t-button></section>
    </template>
    <t-dialog v-model:visible="createVisible" header="创建共享空间" :confirm-btn="{ content: '创建', disabled: !name.trim(), loading: busy }" @confirm="run(create)"><label>名称<t-input v-model="name" :maxlength="100" /></label><label>说明<t-textarea v-model="description" :maxlength="2000" /></label><p>创建后你是空间所有者，邀请的成员默认是使用者。</p></t-dialog>
    <SpaceInviteDialog v-if="selected" v-model:visible="inviteVisible" :space-id="selected.id" :space-name="selected.name" @invited="run(refresh)" />
    <t-dialog v-model:visible="shareVisible" header="共享我的资源" :confirm-btn="{ content: '确认共享', disabled: !resource, loading: busy }" @confirm="run(share)"><label>选择资源<select v-model="choice"><option value="" disabled>请选择</option><option v-for="r in candidates" :key="`${r.kind}:${r.id}`" :value="`${r.kind}:${r.id}`">{{ r.kind === 'agent' ? '智能体' : '知识库' }} · {{ r.name }}</option></select></label><label class="check"><input v-model="allowEdit" type="checkbox" />{{ resource?.kind === 'agent' ? '允许修改智能体配置' : '允许修改原件' }}</label><p v-if="allowEdit" class="notice">编辑者及以上成员的修改将影响原件，以及使用它的其他共享空间；{{ resource?.kind === 'knowledge_base' ? '文档修改会保留人员、时间与版本记录。' : '保存前请核对提示词及知识库范围。' }}</p><label v-if="resource?.kind === 'knowledge_base'" class="check"><input v-model="allowDownload" type="checkbox" />允许下载原文件</label><p v-if="resource?.kind === 'agent'">智能体必须明确选择知识库；所选知识库须先向此空间授权，不会自动共享你的全部个人资料。</p></t-dialog>
    <KnowledgeBaseEditorModal :visible="kbCreateVisible" mode="create" :owner-space-id="selected?.id" @update:visible="kbCreateVisible = $event" @success="run(refresh)" />
    <AgentEditorModal :visible="agentEditorVisible" :mode="editingAgent ? 'edit' : 'create'" :agent="editingAgent" :owner-space-id="editingAgent ? undefined : selected?.id" @update:visible="agentEditorVisible = $event" @success="run(refresh)" />
    <t-dialog v-model:visible="recycleVisible" header="知识库与智能体回收站" :footer="false"><p>仅显示你有管理权限的资源。恢复知识库同时恢复此次删除的文档与 Wiki。</p><p v-if="!recycledResources.length">回收站为空</p><div v-for="r in recycledResources" :key="r.resource_id" class="member"><div>{{ r.title }}<small>{{ when(r.expires_at) }} 到期</small></div><t-button @click="run(async () => { await post('/api/v1/spaces/recycled-resources/restore', { kind: r.kind, id: r.resource_id, tenant_id: r.tenant_id }); await openRecycle(); await refresh() })">恢复</t-button></div></t-dialog>
  </main>
</template>

<style scoped>
.source-capabilities{display:block;margin-top:4px}
.spaces-page{padding:30px;max-width:1400px;margin:0 auto;height:100%;min-height:0;color:var(--td-text-color-primary);width:100%;box-sizing:border-box;overflow:auto}header{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:28px}h1{font-size:26px;margin:0 0 10px}h2{font-size:17px}p{line-height:1.7;color:var(--td-text-color-secondary)}button,select{font:inherit}.back{border:0;background:none;color:#087f8c;margin-bottom:15px;cursor:pointer}.actions{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.space-grid,.resource-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:18px}.space-card,.resource-card{padding:22px;border:1px solid var(--td-component-stroke);border-radius:12px;background:var(--td-bg-color-container);text-align:left}.space-card{cursor:pointer}.space-card:hover{border-color:#087f8c}.space-card strong{font-size:18px}.space-card span,small{font-size:12px;color:var(--td-text-color-secondary)}.tabs{display:flex;gap:24px;border-bottom:1px solid var(--td-component-stroke);margin-bottom:24px}.tabs button{padding:12px 4px;border:0;border-bottom:2px solid transparent;background:none;color:inherit;cursor:pointer}.tabs .active{color:#087f8c;border-color:#087f8c}.member,.event{display:flex;gap:18px;align-items:center;padding:18px 8px;border-bottom:1px solid var(--td-component-stroke)}.member>div{flex:1}.member small{display:block;margin-top:5px}.event{flex-wrap:wrap;font-size:13px}.event time{color:var(--td-text-color-secondary)}label{display:block;margin:16px 0}select{display:block;border:1px solid var(--td-component-stroke);border-radius:6px;padding:9px;background:var(--td-bg-color-container);color:inherit;max-width:100%}.check{display:flex;gap:8px;align-items:center}.notice,.error{color:#a64a12}.invitations{padding:16px;border:1px solid #bdd9db;border-radius:10px;margin-bottom:20px}.invitations>div{display:flex;justify-content:space-between;gap:15px;padding:12px 0}.empty{padding:32px;grid-column:1/-1}.resource-card details{font-size:13px;margin:16px 0}.resource-card h2{overflow-wrap:anywhere}@media(max-width:700px){.spaces-page{padding:18px}header{align-items:flex-start;flex-direction:column}.member{flex-wrap:wrap}.member>div{min-width:160px}}
</style>
