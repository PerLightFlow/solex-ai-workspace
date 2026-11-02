<script setup lang="ts">
import { ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { useKnowledgeActivity } from '@/composables/useKnowledgeActivity';
import { attributionTime, documentActionLabel, documentActorLabel, documentFieldLabels } from '@/utils/knowledgeAttribution';

const props = defineProps<{ knowledgeId: string }>();
const { t } = useI18n();
const expanded = ref(false);
const { entries, loading, failed, loaded, nextCursor, loadMore, refresh, retry } = useKnowledgeActivity(() => props.knowledgeId, () => expanded.value);
defineExpose({ refresh });
</script>

<template>
  <section class="document-activity">
    <button class="document-activity__toggle" type="button" :aria-expanded="expanded" @click="expanded = !expanded">
      <t-icon :name="expanded ? 'chevron-down' : 'chevron-right'" />
      <span>{{ t('documentActivity.title') }}</span>
    </button>
    <div v-if="expanded" class="document-activity__body">
      <p class="document-activity__hint">{{ t('documentActivity.hint') }}</p>
      <ol v-if="entries.length" class="document-activity__entries">
        <li v-for="entry in entries" :key="entry.id">
          <div class="document-activity__heading"><strong>{{ t(documentActionLabel(entry.action)) }}</strong><span>{{ documentActorLabel(entry, t) }}</span></div>
          <time :datetime="entry.created_at">{{ attributionTime(entry.created_at, t('documentActivity.notRecorded')) }}</time>
          <p v-if="documentFieldLabels(entry.changed_fields || []).length">{{ t('documentActivity.changedFields') }}{{ documentFieldLabels(entry.changed_fields).map(key => t(key)).join('、') }}</p>
        </li>
      </ol>
      <p v-if="failed" role="alert" class="document-activity__error">{{ t('documentActivity.loadFailed') }}</p>
      <p v-else-if="loaded && !entries.length">{{ t('documentActivity.empty') }}</p>
      <div class="document-activity__actions">
        <t-button v-if="failed" size="small" variant="outline" :loading="loading" @click="retry">{{ t('documentActivity.retry') }}</t-button>
        <t-button v-else-if="nextCursor" size="small" variant="outline" :loading="loading" @click="loadMore()">{{ t('documentActivity.loadMore') }}</t-button>
        <t-button v-if="loaded" size="small" variant="text" :disabled="loading" @click="refresh">{{ t('documentActivity.refresh') }}</t-button>
        <t-loading v-if="loading && !loaded" size="small" />
      </div>
    </div>
  </section>
</template>

<style scoped lang="less">
.document-activity {
  border-top: 1px solid var(--td-component-stroke); padding-top: 12px;
  &__toggle { display: flex; gap: 8px; align-items: center; border: 0; padding: 4px 0; background: transparent; color: var(--td-text-color-primary); font: inherit; font-weight: 600; cursor: pointer; }
  &__body { padding-top: 8px; font-size: 13px; }
  &__hint, time { color: var(--td-text-color-placeholder); font-size: 12px; }
  &__entries { list-style: none; margin: 14px 0; padding: 0; }
  li { border-left: 2px solid var(--td-component-stroke); padding: 0 0 16px 14px; overflow-wrap: anywhere; }
  li p { margin: 5px 0 0; color: var(--td-text-color-secondary); }
  &__heading { display: flex; flex-wrap: wrap; gap: 8px 16px; margin-bottom: 5px; }
  &__actions { display: flex; gap: 8px; align-items: center; }
  &__error { color: var(--td-error-color); }
}
</style>
