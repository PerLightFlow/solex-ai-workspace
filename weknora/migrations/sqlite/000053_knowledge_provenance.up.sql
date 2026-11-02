ALTER TABLE knowledges ADD COLUMN created_by VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN created_by_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN last_modified_by VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN last_modified_by_name VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE knowledges ADD COLUMN last_modified_at DATETIME;
CREATE INDEX IF NOT EXISTS idx_audit_logs_knowledge_target ON audit_logs (tenant_id, target_type, target_id, id DESC);

UPDATE knowledges SET (created_by, created_by_name) = (
    SELECT a.actor_user_id, CASE WHEN instr(COALESCE(NULLIF(json_extract(a.details, '$.actor_name'), ''), u.username, ''), '@') > 0 THEN '' ELSE substr(trim(replace(replace(replace(COALESCE(NULLIF(json_extract(a.details, '$.actor_name'), ''), u.username, ''), char(10), ''), char(13), ''), char(9), '')), 1, 100) END
    FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id
    WHERE a.tenant_id = knowledges.tenant_id AND a.target_id = knowledges.id
      AND a.target_type = 'knowledge' AND a.action = 'knowledge.created'
      AND a.actor_user_id <> '' AND a.actor_user_id NOT LIKE 'system-%'
      AND a.outcome IN ('success', 'accepted')
    ORDER BY a.created_at ASC, a.id ASC LIMIT 1
) WHERE EXISTS (
    SELECT 1 FROM audit_logs a WHERE a.tenant_id = knowledges.tenant_id AND a.target_id = knowledges.id
      AND a.target_type = 'knowledge' AND a.action = 'knowledge.created'
      AND a.actor_user_id <> '' AND a.actor_user_id NOT LIKE 'system-%' AND a.outcome IN ('success', 'accepted')
);

UPDATE knowledges SET (last_modified_by, last_modified_by_name, last_modified_at) = (
    SELECT a.actor_user_id, CASE WHEN instr(COALESCE(NULLIF(json_extract(a.details, '$.actor_name'), ''), u.username, ''), '@') > 0 THEN '' ELSE substr(trim(replace(replace(replace(COALESCE(NULLIF(json_extract(a.details, '$.actor_name'), ''), u.username, ''), char(10), ''), char(13), ''), char(9), '')), 1, 100) END, a.created_at
    FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id
    WHERE a.tenant_id = knowledges.tenant_id AND a.target_id = knowledges.id
      AND a.target_type = 'knowledge' AND a.action IN ('knowledge.created', 'knowledge.updated')
      AND a.actor_user_id <> '' AND a.actor_user_id NOT LIKE 'system-%'
      AND a.outcome IN ('success', 'accepted')
    ORDER BY a.created_at DESC, a.id DESC LIMIT 1
) WHERE EXISTS (
    SELECT 1 FROM audit_logs a WHERE a.tenant_id = knowledges.tenant_id AND a.target_id = knowledges.id
      AND a.target_type = 'knowledge' AND a.action IN ('knowledge.created', 'knowledge.updated')
      AND a.actor_user_id <> '' AND a.actor_user_id NOT LIKE 'system-%' AND a.outcome IN ('success', 'accepted')
);
