import assert from 'node:assert/strict';
import test from 'node:test';
import { attributionName, attributionTime, pickKnowledgeAttribution, documentActorLabel, documentActionLabel, documentFieldLabels } from './knowledgeAttribution';

test('background updated_at never replaces upload or human-modification timestamps', () => {
  const document = { created_at: '2026-01-01T00:00:00Z', last_modified_at: '2026-02-01T00:00:00Z', updated_at: '2026-11-02T00:00:00Z' };
  const result = pickKnowledgeAttribution(document);
  assert.equal(result.created_at, document.created_at);
  assert.equal(result.last_modified_at, document.last_modified_at);
  assert.equal(pickKnowledgeAttribution({ ...document, last_modified_at: null }).last_modified_at, null);
  assert.equal('updated_at' in result, false);
});

test('unknown identities remain unrecorded without using account IDs as names', () => {
  const result = pickKnowledgeAttribution({ created_by: 'private-id', last_modified_by: 'private-editor' });
  assert.equal(attributionName(result.created_by_name, 'Not recorded'), 'Not recorded');
  assert.equal(attributionName(result.last_modified_by_name, 'Not recorded'), 'Not recorded');
  assert.equal(attributionName('  Alice  ', 'Unknown'), 'Alice');
  for (const time of [null, undefined, '', 'not-a-date']) assert.equal(attributionTime(time, 'Not recorded'), 'Not recorded');
});

test('actor kind distinguishes system actions from unnamed or legacy users', () => {
  const t = (key: string) => key;
  assert.equal(documentActorLabel({ actor_kind: 'system', actor_name: 'internal-service-id' }, t), 'documentActivity.system');
  assert.equal(documentActorLabel({ actor_kind: 'user', actor_name: '张三' }, t), '张三');
  assert.equal(documentActorLabel({ actor_kind: 'user', actor_name: '' }, t), 'documentActivity.unknownUser');
  assert.equal(documentActorLabel({ actor_kind: 'unknown', actor_name: 'untrusted-id' }, t), 'documentActivity.unknownUser');
});

test('activity labels expose known business fields and do not print unknown raw payload fields', () => {
  assert.equal(documentActionLabel('knowledge.updated'), 'documentActivity.actions.updated');
  assert.equal(documentActionLabel('raw.private.action'), 'documentActivity.actions.other');
  assert.deepEqual(documentFieldLabels(['title', 'chunks', 'private.secret', 'title']), ['documentActivity.fields.title', 'documentActivity.fields.chunks']);
});
