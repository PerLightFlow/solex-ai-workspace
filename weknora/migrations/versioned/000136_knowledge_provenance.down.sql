DROP INDEX IF EXISTS idx_audit_logs_knowledge_target;
ALTER TABLE knowledges DROP COLUMN IF EXISTS last_modified_at;
ALTER TABLE knowledges DROP COLUMN IF EXISTS last_modified_by_name;
ALTER TABLE knowledges DROP COLUMN IF EXISTS last_modified_by;
ALTER TABLE knowledges DROP COLUMN IF EXISTS created_by_name;
ALTER TABLE knowledges DROP COLUMN IF EXISTS created_by;
