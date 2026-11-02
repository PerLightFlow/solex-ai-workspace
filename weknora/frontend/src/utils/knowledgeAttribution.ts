/** Human provenance is independent of background parsing/indexing timestamps. */
export interface KnowledgeAttribution {
  created_at?: string | null;
  created_by?: string;
  created_by_name?: string;
  last_modified_by?: string;
  last_modified_by_name?: string;
  last_modified_at?: string | null;
}

export function pickKnowledgeAttribution(data: KnowledgeAttribution = {}): KnowledgeAttribution {
  return {
    created_at: data.created_at ?? null,
    created_by: data.created_by || '',
    created_by_name: data.created_by_name?.trim() || '',
    last_modified_by: data.last_modified_by || '',
    last_modified_by_name: data.last_modified_by_name?.trim() || '',
    last_modified_at: data.last_modified_at ?? null,
  };
}

export function attributionName(name: string | null | undefined, unknown: string): string {
  return name?.trim() || unknown;
}

export function attributionTime(value: string | null | undefined, unknown: string): string {
  if (!value) return unknown;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return unknown;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

const ACTION_LABELS: Record<string, string> = {
  'knowledge.created': 'documentActivity.actions.created',
  'knowledge.updated': 'documentActivity.actions.updated',
  'knowledge.reparse_started': 'documentActivity.actions.reparse',
  'knowledge.parse_canceled': 'documentActivity.actions.cancelParse',
  'knowledge.move_completed': 'documentActivity.actions.moved',
  'knowledge.deleted': 'documentActivity.actions.deleted',
};
const FIELD_LABELS: Record<string, string> = {
  title: 'documentActivity.fields.title', description: 'documentActivity.fields.description',
  custom_metadata: 'documentActivity.fields.customMetadata', content: 'documentActivity.fields.content',
  file: 'documentActivity.fields.file', tags: 'documentActivity.fields.tags',
  folder_path: 'documentActivity.fields.folder', knowledge_base_id: 'documentActivity.fields.knowledgeBase',
  chunks: 'documentActivity.fields.chunks',
};
export const documentActionLabel = (action: string) => ACTION_LABELS[action] || 'documentActivity.actions.other';
export const documentFieldLabels = (fields: string[]) => [...new Set(fields.map(field => FIELD_LABELS[field]).filter(Boolean))];

export function documentActorLabel(actor: { actor_kind: string; actor_name?: string }, t: (key: string) => string): string {
  if (actor.actor_kind === 'system') return t('documentActivity.system');
  if (actor.actor_kind !== 'user') return t('documentActivity.unknownUser');
  return attributionName(actor.actor_name, t('documentActivity.unknownUser'));
}
