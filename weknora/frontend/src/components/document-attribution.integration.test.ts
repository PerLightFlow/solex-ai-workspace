import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';
import { pickKnowledgeAttribution } from '../utils/knowledgeAttribution';

const source = readFileSync(new URL('./doc-content.vue', import.meta.url), 'utf8');

test('manual edit attribution refresh ignores old responses and emits server provenance to the file list', async () => {
  const functionSource = source.slice(source.indexOf('const refreshDocumentAttribution ='), source.indexOf('const assistant = useAssistantStore()'));
  const requests: Array<(response: any) => void> = [];
  const events: any[] = [];
  const props = { details: { id: 'file-a' } as any };
  const refresh = runInNewContext(ts.transpile(`let attributionRefreshGeneration = 0; ${functionSource}\nrefreshDocumentAttribution`), {
    props, pickKnowledgeAttribution,
    getKnowledgeDetails: () => new Promise(resolve => requests.push(resolve)),
    emit: (...args: any[]) => events.push(args), activityPanel: { value: { refresh() {} } },
  });
  const first = refresh(); const second = refresh();
  requests[1]({ data: { created_by_name: 'Uploader', last_modified_by_name: 'Recent editor', last_modified_at: '2026-02-01T00:00:00Z', updated_at: '2099-01-01T00:00:00Z' } });
  await second;
  requests[0]({ data: { last_modified_by_name: 'Stale editor', last_modified_at: '2026-01-01T00:00:00Z' } });
  await first;
  assert.equal(props.details.last_modified_by_name, 'Recent editor');
  assert.equal(props.details.last_modified_at, '2026-02-01T00:00:00Z');
  assert.equal(events.length, 1);
  assert.equal(events[0][0], 'attributionChange');
  assert.equal(events[0][1].id, 'file-a');
  assert.equal(events[0][1].updated_at, undefined);
});

test('readable document details show provenance and activity independently of edit permission', () => {
  assert.match(source, /<DocumentAttribution :document="details"/);
  assert.match(source, /<KnowledgeActivityPanel v-if="visible && details.id"/);
  assert.doesNotMatch(source, /details\.time[^\n]*class="doc-detail-row"/);
});
