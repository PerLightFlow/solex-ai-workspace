<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '@/utils/request'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
const route = useRoute(), router = useRouter()
const kb = computed(() => String(route.params.kbId || ''))
const documents = ref<any[]>([]), recycled = ref<any[]>([]), versions = ref<any[]>([])
const id = ref(''), revision = ref(''), preview = ref<any>(null), busy = ref(false), error = ref('')
const keyword = ref(''), page = ref(1), total = ref(0), mode = ref('versions'), canEdit = ref(false)
const when = (v: string) => new Date(v).toLocaleString()
async function run(fn: () => Promise<unknown>) { if (busy.value) return; busy.value = true; error.value = ''; try { await fn() } catch (e: any) { error.value = e?.response?.data?.message || e?.message || '操作失败' } finally { busy.value = false } }
async function load() {
  const result = await get(`/api/v1/knowledge-bases/${kb.value}/knowledge`, { params: { page: page.value, page_size: 50, keyword: keyword.value } })
  const data = result.data
  documents.value = Array.isArray(data) ? data : (data?.items || data?.data || data?.knowledges || [])
  total.value = result.total || data?.total || documents.value.length
  const permission = await get(`/api/v1/spaces/knowledge-bases/${kb.value}/permission`)
  canEdit.value = permission.data?.edit === true
  if (canEdit.value) recycled.value = (await get(`/api/v1/spaces/knowledge-bases/${kb.value}/recycle-bin`)).data || []
}
async function history(value: string) {
  id.value = value; preview.value = null
  const response = await get(`/api/v1/spaces/knowledge/${value}/versions`)
  versions.value = response.data.versions; revision.value = response.data.revision
}
function confirm(body: string, fn: () => Promise<unknown>) {
  const dialog = DialogPlugin.confirm({ header: '恢复内容', body, confirmBtn: '恢复', cancelBtn: '取消', onConfirm: () => { dialog.hide(); void run(fn) } })
}
async function restore(version: number) {
  const result = await post(`/api/v1/spaces/knowledge/${id.value}/restore`, { version, expected_revision: revision.value })
  MessagePlugin.success(result.data?.message || '已恢复为新版本，正在重建索引与 Wiki')
  await history(id.value); await load()
}
onMounted(() => run(load))
</script>
<template>
  <main class="history-page">
    <button class="back" @click="router.push(`/platform/knowledge-bases/${kb}`)">← 返回知识库</button>
    <h1>内容版本与回收站</h1><p>修改原件会影响所有获授权的空间。恢复会生成新版本；解析和 Wiki 更新需要等待后台处理完成。</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <nav><t-button :variant="mode === 'versions' ? 'base' : 'outline'" @click="mode = 'versions'">文档版本</t-button><t-button v-if="canEdit" :variant="mode === 'recycle' ? 'base' : 'outline'" @click="mode = 'recycle'">回收站</t-button></nav>
    <section v-if="mode === 'versions'" class="columns">
      <aside><form @submit.prevent="page = 1; run(load)"><t-input v-model="keyword" placeholder="搜索文件" /><t-button type="submit" variant="outline">搜索</t-button></form><button v-for="d in documents" :key="d.id" class="doc" :class="{ selected: id === d.id }" @click="run(() => history(d.id))">{{ d.title || d.file_name }}</button><t-pagination v-model="page" :total="total" :page-size="50" size="small" :show-page-size="false" @change="run(load)" /></aside>
      <article><p v-if="!id">选择一份文档查看历史。分块与 Wiki 页面仍可在各自编辑器查看、恢复内容版本。</p><p v-else-if="!versions.length">当前为初始版本。后续修改时将保留历史；迁移前未保存的版本无法重建。</p>
        <div v-for="v in versions" :key="v.id" class="version"><div><strong>{{ when(v.created_at) }}</strong><p>{{ v.actor_name || '系统' }} · {{ v.automatic ? '自动整理' : '人工修改' }}<span v-if="v.revision === revision"> · 当前内容</span></p></div><t-button size="small" variant="text" @click="run(async () => { preview = (await get(`/api/v1/spaces/knowledge/${id}/versions/${v.id}`)).data })">查看</t-button><t-button v-if="canEdit && v.revision !== revision" size="small" variant="outline" @click="confirm('将此历史原件恢复为新版本，现有内容也会保留。', () => restore(v.id))">恢复</t-button></div>
        <section v-if="preview" class="preview"><h2>{{ preview.title }}</h2><p>{{ preview.file_name }}</p><pre>{{ preview.content || JSON.stringify(preview.custom_metadata, null, 2) }}</pre></section>
      </article>
    </section>
    <section v-else><p v-if="!recycled.length">回收站为空。</p><div v-for="item in recycled" :key="`${item.kind}:${item.resource_id}`" class="version"><div><strong>{{ item.title }}</strong><p>{{ when(item.deleted_at) }} 删除 · {{ when(item.expires_at) }} 到期</p></div><t-button :disabled="busy" @click="confirm('恢复该原件并重新建立索引。', async () => { const result = await post(`/api/v1/spaces/recycle/${item.resource_id}/restore?kind=${item.kind}`); MessagePlugin.success(result.data?.message || '恢复已提交'); await load() })">恢复</t-button></div></section>
  </main>
</template>
<style scoped>
.history-page{padding:32px;overflow:auto;width:100%;box-sizing:border-box}.back{border:0;background:none;color:#087f8c;cursor:pointer}h1{font-size:24px}p{color:var(--td-text-color-secondary);line-height:1.7}nav,form{display:flex;gap:12px;margin:20px 0}.columns{display:grid;grid-template-columns:280px 1fr;gap:32px}.doc{display:block;text-align:left;width:100%;padding:12px;border:0;background:none;color:inherit;cursor:pointer;overflow-wrap:anywhere}.selected{background:var(--td-brand-color-light)}.version{display:flex;align-items:center;gap:15px;padding:18px 0;border-bottom:1px solid var(--td-component-stroke)}.version>div{flex:1}.version p{margin:5px 0}.preview{padding:20px;background:var(--td-bg-color-secondarycontainer);margin-top:20px}pre{white-space:pre-wrap;overflow-wrap:anywhere}.error{color:#b64437}@media(max-width:700px){.history-page{padding:16px}.columns{grid-template-columns:1fr}.version{flex-wrap:wrap}}
</style>
