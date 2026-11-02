-- Attribution is independent of updated_at, which also changes during parsing.
ALTER TABLE knowledges ADD COLUMN created_by VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN created_by_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN last_modified_by VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN last_modified_by_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN last_modified_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_audit_logs_knowledge_target ON audit_logs (tenant_id, target_type, target_id, id DESC);

-- Only durable, successful/accepted events establish historical attribution.
-- Never infer a document author from its workspace or knowledge-base owner.
WITH creators AS (
    SELECT DISTINCT ON (a.tenant_id, a.target_id)
        a.tenant_id, a.target_id, a.actor_user_id,
        CASE WHEN position('@' in COALESCE(NULLIF(a.details->>'actor_name', ''), u.username, '')) > 0 THEN '' ELSE left(btrim(regexp_replace(COALESCE(NULLIF(a.details->>'actor_name', ''), u.username, ''), '[[:cntrl:]]', '', 'g')), 100) END AS actor_name
    FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id
    WHERE a.target_type = 'knowledge' AND a.action = 'knowledge.created'
      AND a.actor_user_id <> '' AND a.actor_user_id NOT LIKE 'system-%'
      AND a.outcome IN ('success', 'accepted')
    ORDER BY a.tenant_id, a.target_id, a.created_at ASC, a.id ASC
)
UPDATE knowledges k SET created_by = c.actor_user_id, created_by_name = c.actor_name
FROM creators c WHERE k.tenant_id = c.tenant_id AND k.id = c.target_id;

WITH editors AS (
    SELECT DISTINCT ON (a.tenant_id, a.target_id)
        a.tenant_id, a.target_id, a.actor_user_id, a.created_at,
        CASE WHEN position('@' in COALESCE(NULLIF(a.details->>'actor_name', ''), u.username, '')) > 0 THEN '' ELSE left(btrim(regexp_replace(COALESCE(NULLIF(a.details->>'actor_name', ''), u.username, ''), '[[:cntrl:]]', '', 'g')), 100) END AS actor_name
    FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id
    WHERE a.target_type = 'knowledge' AND a.action IN ('knowledge.created', 'knowledge.updated')
      AND a.actor_user_id <> '' AND a.actor_user_id NOT LIKE 'system-%'
      AND a.outcome IN ('success', 'accepted')
    ORDER BY a.tenant_id, a.target_id, a.created_at DESC, a.id DESC
)
UPDATE knowledges k SET last_modified_by = e.actor_user_id, last_modified_by_name = e.actor_name,
    last_modified_at = e.created_at
FROM editors e WHERE k.tenant_id = e.tenant_id AND k.id = e.target_id;
