-- Schema only. Deploy-time migration validates legacy permissions before enabling SOLEX_USER_SPACES.
CREATE TABLE solex_space_members (space_id VARCHAR(36) NOT NULL, user_id VARCHAR(36) NOT NULL, role VARCHAR(16) NOT NULL CHECK (role IN ('owner','admin','editor','viewer')), joined_at TIMESTAMP NOT NULL, PRIMARY KEY(space_id,user_id));
CREATE INDEX solex_space_members_user ON solex_space_members(user_id);
CREATE TABLE solex_space_invitations (id VARCHAR(36) PRIMARY KEY, space_id VARCHAR(36) NOT NULL, email VARCHAR(255) NOT NULL, invited_by VARCHAR(36) NOT NULL, status VARCHAR(16) NOT NULL, created_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL, accepted_at TIMESTAMP);
CREATE INDEX solex_space_invitation_email ON solex_space_invitations(email,status);
CREATE TABLE solex_space_resources (kind VARCHAR(24) NOT NULL, resource_id VARCHAR(36) NOT NULL, source_tenant_id BIGINT NOT NULL, owner_user_id VARCHAR(36) NOT NULL DEFAULT '', owner_space_id VARCHAR(36) NOT NULL DEFAULT '', created_by VARCHAR(36) NOT NULL, created_at TIMESTAMP NOT NULL, PRIMARY KEY(kind,resource_id,source_tenant_id), CHECK ((owner_user_id <> '' AND owner_space_id = '') OR (owner_user_id = '' AND owner_space_id <> '')));
CREATE INDEX solex_space_resources_owner ON solex_space_resources(owner_space_id);
CREATE TABLE solex_space_grants (id VARCHAR(36) PRIMARY KEY, space_id VARCHAR(36) NOT NULL, kind VARCHAR(24) NOT NULL, resource_id VARCHAR(36) NOT NULL, source_tenant_id BIGINT NOT NULL, allow_edit BOOLEAN NOT NULL DEFAULT FALSE, allow_download BOOLEAN NOT NULL DEFAULT FALSE, shared_by VARCHAR(36) NOT NULL, history_from TIMESTAMP NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL, UNIQUE(space_id,kind,resource_id,source_tenant_id));
CREATE INDEX solex_space_grants_resource ON solex_space_grants(kind,resource_id,source_tenant_id);
CREATE TABLE solex_space_events (id BIGSERIAL PRIMARY KEY, space_id VARCHAR(36) NOT NULL, actor_id VARCHAR(36) NOT NULL, action VARCHAR(64) NOT NULL, subject VARCHAR(255) NOT NULL, created_at TIMESTAMP NOT NULL);
CREATE INDEX solex_space_events_space ON solex_space_events(space_id,id);
CREATE TABLE solex_space_migration_state (id INTEGER PRIMARY KEY, completed_at TIMESTAMP, baseline TEXT NOT NULL DEFAULT '');

CREATE TABLE solex_content_versions (id BIGSERIAL PRIMARY KEY, kind VARCHAR(24) NOT NULL, resource_id VARCHAR(36) NOT NULL, tenant_id BIGINT NOT NULL, kb_id VARCHAR(36) NOT NULL, hash VARCHAR(64) NOT NULL, payload TEXT NOT NULL, source_path TEXT NOT NULL DEFAULT '', actor_id VARCHAR(64) NOT NULL, actor_name VARCHAR(100) NOT NULL, automatic BOOLEAN NOT NULL, created_at TIMESTAMP NOT NULL);
CREATE INDEX solex_content_history ON solex_content_versions(kind,resource_id,tenant_id,id);
CREATE TABLE solex_recycled_content (kind VARCHAR(24) NOT NULL, resource_id VARCHAR(36) NOT NULL, tenant_id BIGINT NOT NULL, kb_id VARCHAR(36) NOT NULL, title TEXT NOT NULL, payload TEXT NOT NULL, source_path TEXT NOT NULL DEFAULT '', actor_id VARCHAR(64) NOT NULL, deleted_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL, PRIMARY KEY(kind,resource_id,tenant_id));
CREATE INDEX solex_recycled_expiry ON solex_recycled_content(expires_at);
CREATE TABLE solex_content_retention (id INTEGER PRIMARY KEY, manual_versions INTEGER NOT NULL, automatic_versions INTEGER NOT NULL, recycle_days INTEGER NOT NULL);
INSERT INTO solex_content_retention VALUES(1,30,30,30);
CREATE INDEX solex_version_source ON solex_content_versions(source_path);
CREATE INDEX solex_recycle_source ON solex_recycled_content(source_path);
CREATE TABLE solex_retained_files (id VARCHAR(64) PRIMARY KEY, tenant_id BIGINT NOT NULL, kb_id VARCHAR(36) NOT NULL, path TEXT NOT NULL, created_at TIMESTAMP NOT NULL);

CREATE TABLE solex_space_state (space_id VARCHAR(36) PRIMARY KEY, archived BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMP NOT NULL);
CREATE TABLE solex_resource_link_principals (token_hash VARCHAR(64) PRIMARY KEY, user_id VARCHAR(36) NOT NULL, tenant_id BIGINT NOT NULL, expires_at TIMESTAMP NOT NULL);
CREATE INDEX solex_resource_link_expiry ON solex_resource_link_principals(expires_at);
