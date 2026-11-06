<template>
  <t-dialog :visible="true" :header="accountActive ? '停用账号与交接' : '账号交接'" width="720px"
    :z-index="3100" :footer="false" :close-on-overlay-click="false" :close-on-esc-keydown="false"
    :close-btn="!busy" @close="close">
    <div class="account-handover" aria-live="polite">
      <div class="identity"><strong>{{ user.username }}</strong><span>{{ user.email }}</span>
        <t-tag :theme="accountActive ? 'primary' : 'warning'">{{ accountActive ? '账号有效' : '账号已停用' }}</t-tag>
      </div>
      <p v-if="accountActive">停用后，该账号将无法登录，现有登录会话也会失效。请先核对交接清单，停用后可在此指定接任人。</p>
      <p v-else>在此完成共享空间和已共享原件的交接；也可以稍后从用户列表的“交接”继续。</p>
      <p class="hint">仅处理共享内容。未共享的个人资料和私人聊天保持私有；原有共享授权保留，操作记入空间动态。</p>
      <t-alert v-if="error" theme="error" :message="error" />
      <t-button v-if="error" variant="text" :disabled="!!busy" @click="refresh">重新加载清单</t-button>
      <p v-if="busy === 'load'">正在读取交接清单…</p>
      <template v-if="preview">
        <h3>共享空间 <span>{{ preview.spaces.length }}</span></h3>
        <p v-if="!preview.spaces.length" class="empty">没有需要转交所有权的共享空间</p>
        <div v-for="space in preview.spaces" :key="space.id" class="handover-row">
          <strong>{{ space.name }}</strong>
          <template v-if="!accountActive">
            <select v-model="spaceTargets[space.id]" :disabled="!!busy" :aria-label="`${space.name}的接任人`">
              <option value="">选择接任人</option>
              <option v-for="u in successors" :key="u.id" :value="u.id">{{ u.username }} · {{ u.email }}</option>
            </select>
            <t-button size="small" :disabled="!!busy || !spaceTargets[space.id]" @click="transferSpace(space.id)">转交所有权</t-button>
          </template>
        </div>
        <h3>已共享的个人原件 <span>{{ resources.length }}</span></h3>
        <p v-if="resources.length" class="hint">原件可转为所属空间的资源，由该空间管理。共享到多个空间的原件，只需选择一个归属空间；其他共享关系保留。建议先处理知识库，再处理引用它的智能体。</p>
        <p v-else class="empty">没有需要移交的个人原件</p>
        <div v-for="item in resources" :key="item.key" class="handover-row resource-row">
          <div><strong>{{ item.resource.name }}</strong>
            <small>{{ item.resource.kind === 'agent' ? '智能体' : '知识库' }} · 已共享到 {{ item.spaces.map(s => s.label).join('、') }}</small>
          </div>
          <template v-if="!accountActive">
            <select v-model="resourceTargets[item.key]" :disabled="!!busy" :aria-label="`${item.resource.name}的归属空间`">
              <option value="">选择归属空间</option>
              <option v-for="space in item.spaces" :key="space.value" :value="space.value">{{ space.label }}</option>
            </select>
            <t-button size="small" :disabled="!!busy || !resourceTargets[item.key]" @click="transferResource(item)">移交原件</t-button>
          </template>
        </div>
        <t-alert v-if="!accountActive && !pending && !error && !busy" theme="success" message="该账号当前没有待交接的共享内容。" />
      </template>
      <div class="footer">
        <t-button variant="outline" :disabled="!!busy" @click="close">{{ accountActive ? '取消' : (pending || error ? '稍后处理' : '完成') }}</t-button>
        <t-button v-if="accountActive" theme="danger" :loading="busy === 'disable'" :disabled="!!busy || !preview || !!error" @click="disableAccount">
          {{ pending ? '停用账号，继续交接' : '确认停用' }}
        </t-button>
      </div>
    </div>
  </t-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { listAllUsers, setUserStatus, type SystemAdminUser } from '@/api/system'
import { getAccountHandover, handoverResource, handoverSpace, type AccountHandover } from '@/api/spaces/handover'
import { groupHandoverResources } from './accountHandover'

const props = defineProps<{ user: SystemAdminUser }>()
const emit = defineEmits<{ close: []; 'status-changed': [boolean]; 'handover-loaded': [AccountHandover]; announced: [string] }>()
const accountActive = ref(props.user.is_active)
const preview = ref<AccountHandover | null>(null)
const successors = ref<SystemAdminUser[]>([])
const spaceTargets = ref<Record<string, string>>({})
const resourceTargets = ref<Record<string, string>>({})
const busy = ref('')
const error = ref('')
const resources = computed(() => groupHandoverResources(preview.value?.resources || []))
const pending = computed(() => (preview.value?.spaces.length || 0) + resources.value.length)
let disposed = false

function message(e: any) { return e?.response?.data?.message || e?.response?.data?.error || e?.message || '操作失败，请重试' }
function close() { if (!busy.value) emit('close') }
function announce(text: string) { MessagePlugin.success(text); emit('announced', text) }
async function load() {
  const result = await getAccountHandover(props.user.id)
  if (disposed) return
  if (result.user_id !== props.user.id) throw new Error('交接账号不匹配，请重新打开窗口')
  preview.value = result
  emit('handover-loaded', result)
  accountActive.value = result.is_active
  emit('status-changed', result.is_active)
  for (const r of resources.value) {
    if (!r.spaces.some(s => s.value === resourceTargets.value[r.key])) {
      resourceTargets.value[r.key] = r.spaces.length === 1 ? r.spaces[0].value : ''
    }
  }
  if (!accountActive.value && result.spaces.length) {
    const candidates: SystemAdminUser[] = []
    let offset = 0
    for (;;) {
      const page = await listAllUsers({ offset, limit: 200 })
      if (disposed) return
      candidates.push(...page.users.filter(u => u.is_active && u.id !== props.user.id))
      if (!page.has_more) break
      if (!page.users.length) throw new Error('接任账号列表未完整加载，请重试')
      offset += page.users.length
    }
    successors.value = candidates
    for (const space of result.spaces) {
      if (!candidates.some(u => u.id === spaceTargets.value[space.id])) spaceTargets.value[space.id] = ''
    }
  }
}
async function refresh() {
  if (busy.value) return
  busy.value = 'load'; error.value = ''
  try { await load() } catch (e) { error.value = message(e) }
  finally { busy.value = '' }
}
async function disableAccount() {
  if (busy.value || !preview.value || !accountActive.value) return
  busy.value = 'disable'; error.value = ''
  try {
    const response = await setUserStatus({ user_id: props.user.id, is_active: false })
    accountActive.value = response.is_active
    emit('status-changed', response.is_active)
    announce(`账号 ${props.user.username} 已停用`)
    try { await load() } catch (e) { error.value = `账号已停用，但交接清单加载失败：${message(e)}` }
  } catch (e) { error.value = message(e) }
  finally { busy.value = '' }
}
async function transfer(action: () => Promise<unknown>, success: string) {
  if (busy.value || accountActive.value) return
  busy.value = 'transfer'; error.value = ''
  try {
    await action()
    announce(success)
    try { await load() } catch (e) { error.value = `交接已完成，但清单刷新失败：${message(e)}`; preview.value = null }
  } catch (e) {
    error.value = message(e)
    // Refresh after a concurrent ownership/status change, retaining the error.
    try { await load() } catch { preview.value = null }
  } finally { busy.value = '' }
}
function transferSpace(id: string) {
  const target = spaceTargets.value[id]
  if (target) return transfer(() => handoverSpace(id, target, props.user.id), '共享空间所有权已转交')
}
function transferResource(item: ReturnType<typeof groupHandoverResources>[number]) {
  const target = resourceTargets.value[item.key]
  if (target) return transfer(() => handoverResource(item.resource, target, props.user.id), '原件已移交给所选空间')
}
function escape(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); close() }
}
onMounted(() => { window.addEventListener('keydown', escape, true); refresh() })
onBeforeUnmount(() => { disposed = true; window.removeEventListener('keydown', escape, true) })
</script>

<style scoped lang="less">
.account-handover {
  max-height: calc(100vh - 210px); overflow-y: auto; padding-right: 4px;
  color: var(--td-text-color-primary);
  .identity { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
  .identity span, .hint, small, .empty { color: var(--td-text-color-secondary); }
  p { line-height: 1.7; margin: 12px 0; }
  .hint, small { font-size: 12px; }
  h3 { font-size: 15px; margin: 22px 0 10px; span { color: var(--td-text-color-secondary); margin-left: 8px; } }
  .handover-row { display: flex; gap: 12px; align-items: center; border-bottom: 1px solid var(--td-component-stroke); padding: 12px 0;
    > strong, > div { flex: 1; min-width: 0; overflow-wrap: anywhere; }
    small { display: block; margin-top: 6px; }
  }
  select { max-width: 230px; min-width: 150px; padding: 7px; border: 1px solid var(--td-component-stroke); border-radius: 6px; background: var(--td-bg-color-container); color: inherit; }
  .footer { display: flex; justify-content: flex-end; gap: 12px; margin-top: 24px; }
  @media (max-width: 650px) { .handover-row { flex-wrap: wrap; > div, > strong { flex-basis: 100%; } } select { max-width: 100%; } }
}
</style>
