<template>
  <div class="truncation-notice" role="status">
    <p>{{ t(`agentStream.toolTruncation.${status === 'recovered' ? 'recoveredHint' : status === 'failed' ? 'failedHint' : 'hint'}`) }}</p>
    <details v-if="details">
      <summary>{{ t('agentStream.toolTruncation.details') }}</summary>
      <pre>{{ details }}</pre>
    </details>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ status: 'retrying' | 'recovered' | 'failed'; details?: string }>()
const { t } = useI18n()
</script>

<style scoped>
.truncation-notice { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); line-height: 1.6; }
p { margin: 0 0 8px; }
summary { cursor: pointer; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; font-size: var(--app-text-xs); }
</style>
