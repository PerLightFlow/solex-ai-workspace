<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { get, put } from '@/utils/request'
import { MessagePlugin } from 'tdesign-vue-next'

const value = ref({ manual_versions: 30, automatic_versions: 30, recycle_days: 30 })
const loading = ref(true), loaded = ref(false), busy = ref(false), error = ref('')
const inRange = (value: number, min: number, max: number) => Number.isInteger(value) && value >= min && value <= max
const valid = computed(() => inRange(value.value.manual_versions, 2, 200)
  && inRange(value.value.automatic_versions, 2, 200) && inRange(value.value.recycle_days, 1, 365))

async function load() {
  loading.value = true
  loaded.value = false
  error.value = ''
  try {
    value.value = (await get('/api/v1/spaces/retention')).data
    loaded.value = true
  } catch (e: any) {
    error.value = e?.message || '无法加载内容保留策略，请重试。'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!loaded.value || !valid.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await put('/api/v1/spaces/retention', value.value)
    MessagePlugin.success('全局内容保留策略已保存')
  } catch (e: any) {
    error.value = e?.message || '保存失败，请重试。'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="retention-settings" :aria-busy="loading || busy">
    <h2>内容保留策略</h2>
    <p>由系统管理员统一配置，适用于所有个人空间和共享空间。空间成员不能单独覆盖。</p>
    <t-loading v-if="loading" text="正在加载全局策略…" />
    <template v-else-if="loaded">
      <label>
        <span>每份内容的人工版本数量<small>保留最近的人工修改版本，范围 2–200 个。</small></span>
        <t-input-number v-model="value.manual_versions" :min="2" :max="200" :step="1" :disabled="busy" aria-label="人工版本数量" />
      </label>
      <label>
        <span>每个 Wiki 页面的自动版本数量<small>保留最近的自动整理版本，范围 2–200 个。</small></span>
        <t-input-number v-model="value.automatic_versions" :min="2" :max="200" :step="1" :disabled="busy" aria-label="自动版本数量" />
      </label>
      <label>
        <span>回收站保留天数<small>新删除内容可恢复的期限，范围 1–365 天。</small></span>
        <t-input-number v-model="value.recycle_days" :min="1" :max="365" :step="1" :disabled="busy" aria-label="回收站保留天数" />
      </label>
      <p class="retention-note">新版本和新删除内容按保存后的策略保留。已有回收站项目的到期时间保持不变；已清理的版本或内容无法恢复。</p>
      <t-button :loading="busy" :disabled="!valid" @click="save">保存全局策略</t-button>
    </template>
    <p v-if="error" role="alert" class="retention-error">{{ error }}</p>
    <t-button v-if="!loading && !loaded" variant="outline" @click="load">重新加载</t-button>
  </section>
</template>

<style scoped>
.retention-settings { max-width: 900px; }
h2 { margin: 0 0 12px; font-size: 20px; font-weight: 600; }
p, small { color: var(--td-text-color-secondary); line-height: 1.7; }
label { display: flex; justify-content: space-between; align-items: center; gap: 24px; padding: 22px 0; border-bottom: 1px solid var(--td-component-stroke); }
small { display: block; margin-top: 5px; font-size: 13px; }
.retention-note { margin: 22px 0; }
.retention-error { color: var(--td-error-color); }
@media (max-width: 600px) { label { align-items: flex-start; flex-direction: column; gap: 12px; } }
</style>
