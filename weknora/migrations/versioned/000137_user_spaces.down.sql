DROP TABLE IF EXISTS solex_resource_link_principals;
DROP TABLE IF EXISTS solex_space_state;
DROP TABLE IF EXISTS solex_retained_files;
DROP TABLE IF EXISTS solex_content_retention;
DROP TABLE IF EXISTS solex_recycled_content;
DROP TABLE IF EXISTS solex_content_versions;
-- Reverting this schema would discard ownership and grants. Use a forward-compatible rollback image.
DROP TABLE IF EXISTS solex_space_migration_state;
DROP TABLE IF EXISTS solex_space_events;
DROP TABLE IF EXISTS solex_space_grants;
DROP TABLE IF EXISTS solex_space_resources;
DROP TABLE IF EXISTS solex_space_invitations;
DROP TABLE IF EXISTS solex_space_members;
