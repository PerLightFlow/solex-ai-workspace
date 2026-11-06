<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { inviteMember, listInviteCandidates, type InviteCandidate } from '@/api/spaces'

const props = defineProps<{ visible: boolean; spaceId: string; spaceName: string }>()
const emit = defineEmits<{ 'update:visible': [value: boolean]; invited: [] }>()
const search = ref(''), selectedId = ref(''), error = ref('')
const users = ref<InviteCandidate[]>([])
const offset = ref(0), hasMore = ref(false), loading = ref(false), sending = ref(false)
const selected = computed(() => users.value.find(user => user.id === selectedId.value))
const pageSize = 20
let request = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined

function invalidate() {
  clearTimeout(searchTimer)
  request++
}

async function loadPage(nextOffset = 0) {
  invalidate()
  const version = request, spaceId = props.spaceId
  loading.value = true
  error.value = ''; selectedId.value = ''; users.value = []; hasMore.value = false
  offset.value = nextOffset
  try {
    const page = await listInviteCandidates(spaceId, search.value.trim(), nextOffset, pageSize)
    if (version !== request || !props.visible || props.spaceId !== spaceId) return
    users.value = page.items; hasMore.value = page.has_more
  } catch (e: any) {
    if (version === request && props.visible) error.value = e?.response?.data?.message || e?.message || '加载用户列表失败，请重试'
  } finally {
    if (version === request) loading.value = false
  }
}

function searchChanged() {
  invalidate()
  selectedId.value = ''; users.value = []; error.value = ''; loading.value = true
  searchTimer = setTimeout(() => void loadPage(), 250)
}

function close(visible: boolean) {
  if (!sending.value) emit('update:visible', visible)
}

async function sendInvitation() {
  if (!selected.value || loading.value || sending.value) return
  sending.value = true; error.value = ''
  try {
    await inviteMember(props.spaceId, selected.value.id)
    MessagePlugin.success('邀请已发送，对方接受后将以使用者身份加入')
    emit('invited')
    emit('update:visible', false)
  } catch (e: any) {
    const message = e?.response?.data?.message || e?.message || '发送邀请失败，请重试'
    // Refresh stale selections (disabled account, new member, or pending invite).
    await loadPage(offset.value)
    error.value = message
  } finally {
    sending.value = false
  }
}

watch(() => [props.visible, props.spaceId], () => {
  invalidate()
  search.value = ''; selectedId.value = ''; error.value = ''; users.value = []
  if (props.visible && props.spaceId) void loadPage()
}, { immediate: true })
onBeforeUnmount(invalidate)
</script>

<template>
  <t-dialog :visible="visible" header="邀请用户" width="580px"
    :confirm-btn="{ content: '发送邀请', disabled: !selected || loading, loading: sending }"
    :cancel-btn="{ content: '取消', disabled: sending }" :close-on-overlay-click="false"
    @update:visible="close" @confirm="sendInvitation">
    <div class="invite-picker">
      <p class="intro">选择加入「{{ spaceName }}」的用户</p>
      <t-input v-model="search" clearable :maxlength="100" :disabled="sending"
        placeholder="搜索用户名或邮箱" aria-label="搜索用户名或邮箱" @change="searchChanged" />
      <p class="hint">仅显示可邀请的有效账号，已加入或已有待接受邀请的用户不会重复显示。</p>
      <div v-if="error" class="error" role="alert">{{ error }} <t-button variant="text" :disabled="sending" @click="loadPage(offset)">刷新列表</t-button></div>
      <div class="user-list" role="group" aria-label="可邀请用户" :aria-busy="loading">
        <p v-if="loading" class="empty" role="status">正在加载用户…</p>
        <template v-else-if="users.length">
          <label v-for="user in users" :key="user.id" class="user-row" :class="{ selected: selectedId === user.id }">
            <input v-model="selectedId" type="radio" name="space-invite-user" :value="user.id" :disabled="sending" />
            <span class="identity"><strong>{{ user.username }}</strong><small>{{ user.email }}</small></span>
          </label>
        </template>
        <p v-else-if="!error" class="empty" role="status">{{ search.trim() ? '没有找到匹配的可邀请用户，请调整搜索条件。' : '暂无可邀请用户。新用户注册并启用后会出现在这里。' }}</p>
      </div>
      <div v-if="offset > 0 || hasMore" class="pagination">
        <span>第 {{ Math.floor(offset / pageSize) + 1 }} 页</span>
        <t-button size="small" variant="outline" :disabled="offset === 0 || loading || sending" @click="loadPage(offset - pageSize)">上一页</t-button>
        <t-button size="small" variant="outline" :disabled="!hasMore || loading || sending" @click="loadPage(offset + pageSize)">下一页</t-button>
      </div>
      <div class="invitation-info">
        <span v-if="selected">已选择：<strong>{{ selected.username }}</strong></span>
        <p>对方登录后可在共享空间接受邀请，七天内有效。加入后的默认角色为「使用者」，可在成员管理中调整。</p>
      </div>
    </div>
  </t-dialog>
</template>

<style scoped>
.invite-picker{color:var(--td-text-color-primary)}.intro{margin:0 0 14px;font-size:15px}.hint{font-size:12px;line-height:1.6;color:var(--td-text-color-secondary);margin:10px 0}.user-list{max-height:280px;overflow-y:auto;border:1px solid var(--td-component-stroke);border-radius:8px;min-height:110px}.user-row{display:flex;align-items:center;gap:14px;padding:12px 16px;cursor:pointer;border-bottom:1px solid var(--td-component-stroke)}.user-row:last-child{border-bottom:none}.user-row:hover,.user-row.selected{background:var(--td-brand-color-light)}.user-row input{accent-color:var(--td-brand-color);width:16px;height:16px;flex-shrink:0}.identity{display:flex;flex-direction:column;gap:4px;min-width:0;overflow-wrap:anywhere}.identity strong{font-weight:500}.identity small{color:var(--td-text-color-secondary);font-size:12px}.empty{padding:22px 16px;text-align:center;color:var(--td-text-color-secondary);line-height:1.7}.pagination{display:flex;align-items:center;justify-content:flex-end;gap:10px;margin-top:12px;font-size:12px}.invitation-info{margin-top:16px;padding:12px 14px;border-radius:8px;background:var(--td-bg-color-secondarycontainer);font-size:13px;line-height:1.7}.invitation-info p{color:var(--td-text-color-secondary);margin:0}.invitation-info span{display:block;margin-bottom:6px}.error{color:var(--td-error-color);font-size:13px;margin:8px 0}
</style>
