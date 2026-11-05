<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { get, put } from '@/utils/request'
import { MessagePlugin } from 'tdesign-vue-next'
const value = ref({ manual_versions: 30, automatic_versions: 30, recycle_days: 30 }), busy = ref(false), error = ref('')
onMounted(async () => { try { value.value = (await get('/api/v1/spaces/retention')).data } catch (e: any) { error.value = e.message } })
async function save() { busy.value = true; error.value = ''; try { await put('/api/v1/spaces/retention', value.value); MessagePlugin.success('保留策略已保存') } catch (e: any) { error.value = e.message } finally { busy.value = false } }
</script>
<template><section><h3>内容保留策略</h3><p>系统管理员统一设置。缩短保留期限不会重建已过期的内容。</p><label>每份内容的人工版本<t-input-number v-model="value.manual_versions" :min="2" :max="200" /></label><label>每个 Wiki 页面的自动版本<t-input-number v-model="value.automatic_versions" :min="2" :max="200" /></label><label>回收站保留天数<t-input-number v-model="value.recycle_days" :min="1" :max="365" /></label><p v-if="error" role="alert">{{ error }}</p><t-button :loading="busy" @click="save">保存策略</t-button></section></template>
<style scoped>label{display:flex;justify-content:space-between;gap:24px;margin:20px 0;max-width:520px}p{color:var(--td-text-color-secondary)}</style>
