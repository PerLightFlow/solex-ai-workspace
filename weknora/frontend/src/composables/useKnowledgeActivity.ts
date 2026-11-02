import { onScopeDispose, ref, watch } from 'vue';
import { listKnowledgeActivity, type KnowledgeActivity } from '@/api/knowledge-base';

export function useKnowledgeActivity(
  knowledgeId: () => string,
  active: () => boolean,
  fetchPage = listKnowledgeActivity,
) {
  const entries = ref<KnowledgeActivity[]>([]);
  const nextCursor = ref(0);
  const loading = ref(false);
  const failed = ref(false);
  const loaded = ref(false);
  let generation = 0;
  let lastRequestWasReset = true;
  onScopeDispose(() => { generation++; });

  async function loadMore(reset = false) {
    const id = knowledgeId();
    if (!id || !active() || loading.value || (!reset && loaded.value && !nextCursor.value)) return;
    const requestGeneration = generation;
    const cursor = reset ? 0 : nextCursor.value;
    lastRequestWasReset = reset;
    loading.value = true;
    failed.value = false;
    try {
      const response = await fetchPage(id, { after_id: cursor || undefined, limit: 20 });
      if (requestGeneration !== generation || id !== knowledgeId() || !active()) return;
      if (!response.success || !Array.isArray(response.data)) throw new Error('activity unavailable');
      const previous = reset ? [] : entries.value;
      const seen = new Set(previous.map(entry => entry.id));
      entries.value = [...previous, ...response.data.filter(entry => {
        if (seen.has(entry.id)) return false;
        seen.add(entry.id); return true;
      })];
      nextCursor.value = response.next_cursor && response.next_cursor !== cursor ? response.next_cursor : 0;
      loaded.value = true;
    } catch {
      if (requestGeneration === generation) failed.value = true;
    } finally {
      if (requestGeneration === generation) loading.value = false;
    }
  }

  watch([knowledgeId, active], () => {
    generation++;
    entries.value = []; nextCursor.value = 0; loaded.value = false; failed.value = false; loading.value = false;
    if (active() && knowledgeId()) void loadMore(true);
  }, { immediate: true });

  return { entries, nextCursor, loading, failed, loaded, loadMore, refresh: () => loadMore(true), retry: () => loadMore(lastRequestWasReset) };
}
