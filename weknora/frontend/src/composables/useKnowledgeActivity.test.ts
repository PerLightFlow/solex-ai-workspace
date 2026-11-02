import assert from 'node:assert/strict';
import test, { type TestContext } from 'node:test';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';
import * as vue from 'vue';

const compiled = ts.transpileModule(readFileSync(new URL('./useKnowledgeActivity.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText;
const row = (id: number) => ({ id, action: 'knowledge.updated', actor_kind: 'user', actor_name: 'Alice', created_at: '2026-11-01T00:00:00Z', changed_fields: ['content'] });
const flush = async () => { await new Promise(setImmediate); await vue.nextTick(); };

function setup(t: TestContext) {
  const requests: Array<{ id: string; params: any; resolve: (value: any) => void; reject: (error: Error) => void }> = [];
  const exports: any = {};
  const fetchPage = (id: string, params: any) => new Promise((resolve, reject) => requests.push({ id, params, resolve, reject }));
  runInNewContext(compiled, { exports, require: (name: string) => name === 'vue' ? vue : { listKnowledgeActivity: fetchPage } });
  const id = vue.ref('file-a'); const active = vue.ref(false);
  const scope = vue.effectScope(); t.after(() => scope.stop());
  const state = scope.run(() => exports.useKnowledgeActivity(() => id.value, () => active.value));
  return { id, active, requests, state, scope };
}

test('activity loads only when opened and follows the server cursor without duplicate records', async t => {
  const f = setup(t); assert.equal(f.requests.length, 0);
  f.active.value = true; await flush();
  assert.equal(f.requests[0].id, 'file-a'); assert.equal(f.requests[0].params.limit, 20);
  f.requests[0].resolve({ success: true, data: [row(10), row(9)], next_cursor: 9 }); await flush();
  const more = f.state.loadMore(); assert.equal(f.requests[1].params.after_id, 9);
  f.requests[1].resolve({ success: true, data: [row(9), row(8)], next_cursor: 0 }); await more;
  assert.deepEqual(Array.from(f.state.entries.value, (entry: any) => entry.id), [10, 9, 8]);
  await f.state.loadMore(); assert.equal(f.requests.length, 2);
});

test('paging failures retain entries and retry the same cursor', async t => {
  const f = setup(t); f.active.value = true; await flush();
  f.requests[0].resolve({ success: true, data: [row(10)], next_cursor: 10 }); await flush();
  const more = f.state.loadMore(); f.requests[1].reject(new Error('server unavailable')); await more;
  assert.equal(f.state.failed.value, true); assert.equal(f.state.entries.value.length, 1);
  const retry = f.state.retry(); assert.equal(f.requests[2].params.after_id, 10);
  f.requests[2].resolve({ success: true, data: [row(8)], next_cursor: 0 }); await retry;
  assert.equal(f.state.failed.value, false); assert.equal(f.state.entries.value.length, 2);
});

test('failed first page and failed refresh can be retried even when no next cursor exists', async t => {
  const f = setup(t); f.active.value = true; await flush();
  f.requests[0].resolve({ success: false, data: [], next_cursor: 0 }); await flush();
  assert.equal(f.state.failed.value, true); assert.equal(f.state.loaded.value, false);
  let pending = f.state.retry(); f.requests[1].resolve({ success: true, data: [row(1)], next_cursor: 0 }); await pending;
  pending = f.state.refresh(); f.requests[2].reject(new Error('failed refresh')); await pending;
  assert.equal(f.state.entries.value.length, 1);
  pending = f.state.retry(); assert.equal(f.requests[3].params.after_id, undefined);
  f.requests[3].resolve({ success: true, data: [row(2)], next_cursor: 0 }); await pending;
  assert.equal(f.state.entries.value[0].id, 2);
});

test('switching documents or closing clears old activity and discards pending responses', async t => {
  const f = setup(t); f.active.value = true; await flush();
  f.id.value = 'file-b'; await flush();
  assert.equal(f.requests[1].id, 'file-b'); assert.equal(f.state.entries.value.length, 0);
  f.requests[0].resolve({ success: true, data: [row(99)], next_cursor: 99 }); await flush();
  assert.equal(f.state.entries.value.length, 0);
  f.requests[1].resolve({ success: true, data: [row(2)], next_cursor: 2 }); await flush();
  const pending = f.state.loadMore(); f.active.value = false; await flush();
  f.requests[2].resolve({ success: true, data: [row(1)], next_cursor: 0 }); await pending;
  assert.equal(f.state.entries.value.length, 0); assert.equal(f.state.loaded.value, false);
});
