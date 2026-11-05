<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { get } from '@/utils/request'
import * as api from '@/api/spaces'
import { MessagePlugin } from 'tdesign-vue-next'
const props = defineProps<{ kind: 'agent' | 'knowledge_base'; id: string; tenant?: number }>()
const spaces = ref<api.Space[]>([]), resource = ref<any>(null), grants = ref<any[]>([]), choice = ref(''), edit = ref(false), download = ref(false), busy = ref(false), error = ref('')
async function load() { try { const [s, r] = await Promise.all([api.listSpaces(), get(`/api/v1/spaces/resources/${props.kind}/${props.id}`, { params: { tenant: props.tenant } })]); spaces.value = s.filter(s => !s.archived && api.roleLevel[s.my_role] >= 2); resource.value = r.data; grants.value = r.data.grants || [] } catch (e: any) { error.value = e.message || '无法加载共享授权' } }
watch(choice, value => { const g = grants.value.find(g => g.space_id === value); edit.value = g?.allow_edit === true; download.value = g?.allow_download === true })
async function save(remove = false) {
  if (!resource.value || !choice.value) return
  busy.value = true; error.value = ''
  try { const r = { kind: props.kind, id: props.id, source_tenant_id: resource.value.source_tenant_id }; if (remove) await api.unshareResource(choice.value, r); else await api.shareResource(choice.value, r, edit.value, download.value); MessagePlugin.success(remove ? '已撤回此空间的共享' : '共享授权已保存'); await load() } catch (e: any) { error.value = e.message || '授权操作失败' } finally { busy.value = false }
}
onMounted(load)
</script>
<template><section><h3>共享到空间</h3><p>成员按账号加入。对原件的修改会同步影响所有获授权的空间。</p><p v-if="error" role="alert">{{ error }}</p><template v-if="resource?.access?.manage"><t-select v-model="choice" placeholder="选择共享空间"><t-option v-for="s in spaces" :key="s.id" :value="s.id" :label="s.name" /></t-select><label><input v-model="edit" type="checkbox" /> 允许修改原件（仅编辑者及以上）</label><label v-if="kind === 'knowledge_base'"><input v-model="download" type="checkbox" /> 允许下载原文件</label><p v-if="kind === 'agent'">共享智能体必须选择明确的知识库清单，且这些知识库已经向目标空间授权。</p><t-button :disabled="!choice || busy" @click="save()">保存授权</t-button><t-button variant="text" theme="danger" :disabled="!grants.some(g => g.space_id === choice) || busy" @click="save(true)">撤回共享</t-button></template><p v-else>你可以使用此资源，但不能修改原持有人的共享授权。</p></section></template>
<style scoped>label{display:block;margin:18px 0}p{line-height:1.7;color:var(--td-text-color-secondary)}</style>
