#!/usr/bin/env python3
"""Preview/apply account-space migration. Run with application writers stopped.

Only the additive schema may be applied before preview. The fingerprint binds
approval to the exact inventory; no credentials or document bodies are logged.
"""
import argparse
import hashlib
import json
import re
import subprocess
import uuid


def literal(value):
    if value is None:
        return 'NULL'
    if isinstance(value, bool):
        return 'TRUE' if value else 'FALSE'
    if isinstance(value, int):
        return str(value)
    return "'" + str(value).replace("'", "''") + "'"


def plan(data, enterprise_tenant):
    users = {u['id']: u for u in data['users']}
    homes = {}
    for user in users.values():
        tenant = user['tenant_id']
        if not tenant or tenant in homes:
            raise ValueError('Active accounts need unique home spaces before migration; no changes applied')
        homes[tenant] = user
    for m in data['members']:
        if m['tenant_id'] != users[m['user_id']]['tenant_id']:
            raise ValueError('An active account has another workspace membership; prepare an explicit resource/history mapping first')
    organizations = {o['id']: o for o in data['organizations']}
    members = {}
    level = {'viewer': 1, 'editor': 2, 'admin': 3, 'owner': 4}
    names = {1: 'viewer', 2: 'editor', 3: 'admin'}
    for row in data['organization_members']:
        if row['organization_id'] not in organizations:
            continue
        for m in data['members']:
            if m['tenant_id'] != row['tenant_id']:
                continue
            cap = 3 if m['role'] in ('owner', 'admin') else 2 if m['role'] == 'contributor' else 1
            role = names[min(level[row['role']], cap)]
            key = (row['organization_id'], m['user_id'])
            if key not in members or level[members[key]] < level[role]:
                members[key] = role
    for o in organizations.values():
        if o['owner_id'] not in users:
            raise ValueError('A shared space has an inactive owner; explicit handover is required')
        members[(o['id'], o['owner_id'])] = 'owner'
    enterprise = None
    if enterprise_tenant:
        owner = homes.get(enterprise_tenant)
        if not owner:
            raise ValueError('Enterprise source owner is not active')
        eid = str(uuid.uuid5(uuid.NAMESPACE_URL, f'solex:user-spaces:enterprise:{enterprise_tenant}'))
        enterprise = {'id': eid, 'name': '企业研发知识空间', 'owner_id': owner['id'], 'owner_tenant_id': enterprise_tenant}
        members[(eid, owner['id'])] = 'owner'
    ownership = []
    for kind, table in [('knowledge_base', 'knowledge_bases'), ('agent', 'agents')]:
        for item in data[table]:
            if item['tenant_id'] not in homes:
                raise ValueError('A resource is owned by a tenant without an active home account; explicit handover is required')
            ownership.append({'kind': kind, 'id': item['id'], 'tenant': item['tenant_id'],
                              'user': '' if enterprise and item['tenant_id'] == enterprise_tenant else homes[item['tenant_id']]['id'],
                              'space': enterprise['id'] if enterprise and item['tenant_id'] == enterprise_tenant else ''})
    grants = []
    for kind, table, id_key in [('knowledge_base', 'kb_shares', 'knowledge_base_id'), ('agent', 'agent_shares', 'agent_id')]:
        for g in data[table]:
            org = g['organization_id']
            if org not in organizations or not any(o['kind'] == kind and o['id'] == g[id_key] and o['tenant'] == g['source_tenant_id'] for o in ownership):
                continue
            if not any(m['organization_id'] == org and m['tenant_id'] == g['source_tenant_id'] for m in data['organization_members']):
                continue  # Preserve lapsed-share behavior.
            if kind == 'agent':
                raise ValueError('Existing agent shares require an explicit KB dependency mapping; no changes applied')
            grants.append({'id': g['id'], 'kind': kind, 'resource': g[id_key], 'tenant': g['source_tenant_id'], 'space': org,
                           'edit': g['permission'] in ('admin', 'editor'), 'download': g['permission'] in ('admin', 'editor'),
                           'shared_by': g['shared_by_user_id']})
    return {'enterprise': enterprise, 'members': [{'space': k[0], 'user': k[1], 'role': v} for k, v in sorted(members.items())],
            'ownership': ownership, 'grants': grants, 'personal_accounts': len(users)}


def verify_permissions(data, result):
    """Compare read/edit/download for every active account and live resource."""
    levels = {'viewer': 1, 'editor': 2, 'admin': 3, 'owner': 4}
    role = {(m['space'], m['user']): m['role'] for m in result['members']}
    checked = 0
    for resource in result['ownership']:
        for user in data['users']:
            own = user['tenant_id'] == resource['tenant']
            before = [own, own, own]
            table = 'kb_shares' if resource['kind'] == 'knowledge_base' else 'agent_shares'
            key = 'knowledge_base_id' if resource['kind'] == 'knowledge_base' else 'agent_id'
            for g in data[table]:
                if g[key] != resource['id'] or g['source_tenant_id'] != resource['tenant']:
                    continue
                org = next((o for o in data['organizations'] if o['id'] == g['organization_id']), None)
                source = any(m['organization_id'] == g['organization_id'] and m['tenant_id'] == g['source_tenant_id'] for m in data['organization_members'])
                membership = next((m for m in data['organization_members'] if m['organization_id'] == g['organization_id'] and m['tenant_id'] == user['tenant_id']), None)
                personal = next((m for m in data['members'] if m['user_id'] == user['id'] and m['tenant_id'] == user['tenant_id']), None)
                if org and source and membership and personal:
                    level = min(levels[g['permission']], levels[membership['role']], 1 if personal['role'] == 'viewer' else 3)
                    before = [True, before[1] or level >= 2, before[2] or level >= 2]
            direct = resource['user'] == user['id']
            after = [direct, direct, direct]
            owned_role = role.get((resource['space'], user['id']))
            if owned_role:
                after = [True, levels[owned_role] >= 2, True]
            for g in result['grants']:
                member_role = role.get((g['space'], user['id']))
                if g['kind'] == resource['kind'] and g['resource'] == resource['id'] and g['tenant'] == resource['tenant'] and member_role:
                    after = [True, after[1] or (g['edit'] and levels[member_role] >= 2), after[2] or g['download']]
            if before != after:
                raise ValueError('Migration would change effective read/edit/download permissions; explicit mapping required')
            checked += 1
    return {'account_resource_pairs': checked, 'read_edit_download_unchanged': True}


def apply_sql(data, result, fingerprint):
    rows = ['BEGIN;', "SELECT pg_advisory_xact_lock(hashtext('solex-user-space-migration'));",
            "DO $$ BEGIN IF EXISTS(SELECT 1 FROM solex_space_migration_state WHERE id=1) THEN RAISE EXCEPTION 'Already migrated'; END IF; END $$;"]
    e = result['enterprise']
    if e:
        values = [e['id'], e['name'], '企业公共资料与协作', e['owner_id'], e['owner_tenant_id'], uuid.uuid4().hex]
        rows.append('INSERT INTO organizations(id,name,description,owner_id,owner_tenant_id,invite_code,member_limit,created_at,updated_at) VALUES(' + ','.join(map(literal, values)) + ',200,NOW(),NOW());')
    for m in result['members']:
        rows.append('INSERT INTO solex_space_members(space_id,user_id,role,joined_at) VALUES(' + ','.join(map(literal, [m['space'], m['user'], m['role']])) + ',NOW());')
    for o in result['ownership']:
        rows.append('INSERT INTO solex_space_resources(kind,resource_id,source_tenant_id,owner_user_id,owner_space_id,created_by,created_at) VALUES(' + ','.join(map(literal, [o['kind'], o['id'], o['tenant'], o['user'], o['space'], 'migration'])) + ',NOW());')
    for g in result['grants']:
        rows.append('INSERT INTO solex_space_grants(id,space_id,kind,resource_id,source_tenant_id,allow_edit,allow_download,shared_by,history_from,created_at,updated_at) VALUES(' + ','.join(map(literal, [g['id'], g['space'], g['kind'], g['resource'], g['tenant'], g['edit'], g['download'], g['shared_by']])) + ',NOW(),NOW(),NOW());')
    # Active accounts have no foreign memberships (verified above). Revoking
    # invitations also prevents a stale invite link reintroducing tenant sharing.
    rows += ["UPDATE tenant_members m SET role='owner' FROM users u WHERE u.id=m.user_id AND u.tenant_id=m.tenant_id AND u.is_active AND u.deleted_at IS NULL AND m.deleted_at IS NULL;",
             "UPDATE users SET preferences = COALESCE(preferences,'{}'::jsonb) - 'last_active_tenant_id' WHERE is_active AND deleted_at IS NULL;",
             "UPDATE tenants t SET name=u.username || ' 的个人空间' FROM users u WHERE u.tenant_id=t.id AND u.is_active AND u.deleted_at IS NULL;",
             'INSERT INTO solex_space_migration_state(id,completed_at,baseline) VALUES(1,NOW(),' + literal(fingerprint) + ');', 'COMMIT;']
    return '\n'.join(rows)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--container', default='weknora-native-postgres-1')
    parser.add_argument('--database', default='weknora')
    parser.add_argument('--enterprise-tenant', type=int, default=10000)
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--expected-fingerprint')
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9_-]+', args.database) or not re.fullmatch(r'[A-Za-z0-9_.-]+', args.container):
        raise ValueError('Invalid database/container identifier')
    def sql(query):
        proc = subprocess.run(['docker', 'exec', '-i', args.container, 'psql', '-U', 'weknora', '-d', args.database, '-X', '-At', '-v', 'ON_ERROR_STOP=1'], input=query, text=True, capture_output=True)
        if proc.returncode:
            raise RuntimeError('Migration SQL failed (no transaction was committed); inspect the isolated database diagnostics')
        return proc.stdout.strip()
    queries = {
        'users': 'SELECT id,username,tenant_id FROM users WHERE is_active AND deleted_at IS NULL ORDER BY id',
        'members': "SELECT m.user_id,m.tenant_id,m.role FROM tenant_members m JOIN users u ON u.id=m.user_id AND u.is_active AND u.deleted_at IS NULL WHERE m.deleted_at IS NULL AND m.status='active' ORDER BY m.user_id,m.tenant_id",
        'organizations': 'SELECT id,name,owner_id,owner_tenant_id FROM organizations WHERE deleted_at IS NULL ORDER BY id',
        'organization_members': 'SELECT organization_id,tenant_id,role FROM organization_tenant_members ORDER BY organization_id,tenant_id',
        'knowledge_bases': 'SELECT id,tenant_id,name FROM knowledge_bases WHERE deleted_at IS NULL ORDER BY id',
        'agents': 'SELECT id,tenant_id,name FROM custom_agents WHERE deleted_at IS NULL AND NOT is_builtin ORDER BY id,tenant_id',
        'kb_shares': 'SELECT id,knowledge_base_id,organization_id,source_tenant_id,permission,shared_by_user_id FROM kb_shares WHERE deleted_at IS NULL ORDER BY id',
        'agent_shares': 'SELECT id,agent_id,organization_id,source_tenant_id,permission,shared_by_user_id FROM agent_shares WHERE deleted_at IS NULL ORDER BY id',
    }
    data = {key: json.loads(sql("SELECT COALESCE(json_agg(t),'[]'::json) FROM (" + query + ') t;')) for key, query in queries.items()}
    fingerprint = hashlib.sha256(json.dumps(data, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    result = plan(data, args.enterprise_tenant)
    verify_permissions(data, result)
    if args.apply:
        if args.expected_fingerprint != fingerprint:
            raise ValueError('Inventory changed or expected fingerprint missing; preview again')
        sql(apply_sql(data, result, fingerprint))
    print(json.dumps({'applied': args.apply, 'fingerprint': fingerprint, 'plan': result, 'permission_comparison': verify_permissions(data, result)}, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
