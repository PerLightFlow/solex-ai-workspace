<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { attributionName, attributionTime, type KnowledgeAttribution } from '@/utils/knowledgeAttribution';

defineProps<{ document: KnowledgeAttribution; compact?: boolean }>();
const { t } = useI18n();
</script>

<template>
  <dl class="document-attribution" :class="{ 'document-attribution--compact': compact }">
    <div><dt>{{ t('documentActivity.uploadedBy') }}</dt><dd>{{ attributionName(document.created_by_name, t('documentActivity.notRecorded')) }}</dd></div>
    <div><dt>{{ t('documentActivity.uploadedAt') }}</dt><dd><time :datetime="document.created_at || undefined">{{ attributionTime(document.created_at, t('documentActivity.notRecorded')) }}</time></dd></div>
    <div><dt>{{ t('documentActivity.modifiedBy') }}</dt><dd>{{ attributionName(document.last_modified_by_name, t('documentActivity.notRecorded')) }}</dd></div>
    <div><dt>{{ t('documentActivity.modifiedAt') }}</dt><dd><time :datetime="document.last_modified_at || undefined">{{ attributionTime(document.last_modified_at, t('documentActivity.notRecorded')) }}</time></dd></div>
  </dl>
</template>

<style scoped lang="less">
.document-attribution {
  display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 10px 18px;
  margin: 0; font-size: 13px; color: var(--td-text-color-primary);
  > div { min-width: 0; }
  dt { color: var(--td-text-color-placeholder); margin-bottom: 3px; font-size: 12px; }
  dd { margin: 0; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
  &--compact { margin-top: 8px; gap: 5px 12px; font-size: 11px; color: var(--td-text-color-secondary); }
  &--compact dt { display: inline; margin-right: 5px; font-size: inherit; }
  &--compact dd { display: inline; }
}
@media (max-width: 480px) { .document-attribution { grid-template-columns: 1fr; } }
</style>
