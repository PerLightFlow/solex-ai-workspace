"""Synthetic API acceptance against an isolated database; no model calls."""
import secrets
from api_client import Client, ApiError, save_json


def expect(client, status, method, path, data=None):
    try:
        client.call(method, path, data)
    except ApiError as error:
        assert error.status in (status if isinstance(status, tuple) else (status,)), (method, path, error.status)
    else:
        raise AssertionError('Expected denial: ' + method + ' ' + path)


def run(base, admin_credentials, state):
    if base != 'http://127.0.0.1:18084':
        raise ValueError('Handover mutation acceptance requires the isolated candidate')
    admin = Client(base)
    admin.login(admin_credentials)
    users, clients, identities = [], [], []
    for label in ('owner', 'successor', 'outsider'):
        identity = {'username': '__handover_' + label + '_' + secrets.token_hex(2),
                    'email': 'handover-' + secrets.token_hex(5) + '@example.invalid',
                    'password': secrets.token_urlsafe(20) + 'Aa1!'}
        client = Client(base)
        users.append(client.call('POST', '/auth/register', identity)['user'])
        client.login(identity)
        clients.append(client); identities.append(identity)
    save_json(state/'handover-fixtures.json', {'users': users, 'identities': identities})
    owner, successor, outsider = clients
    uid, sid = users[0]['id'], users[1]['id']
    tenant = users[0]['tenant_id']
    spaces = [owner.call('POST', '/spaces', {'name': '交接验收-' + label})['data']['id'] for label in ('研发', '协作')]
    for space in spaces:
        invite = owner.call('POST', f'/spaces/{space}/invitations', {'email': identities[1]['email']})['data']['id']
        successor.call('POST', '/spaces/invitations/' + invite + '/accept')
    unrelated = outsider.call('POST', '/spaces', {'name': '交接验收-无关空间'})['data']['id']
    kb = owner.call('POST', '/knowledge-bases', {'name': '交接验收-共享原件', 'type': 'document'})['data']['id']
    private = owner.call('POST', '/knowledge-bases', {'name': '交接验收-私有原件', 'type': 'document'})['data']['id']
    agent = owner.call('POST', '/agents', {'name': '交接验收-共享助手', 'config': {
        'agent_mode': 'smart-reasoning', 'kb_selection_mode': 'selected', 'knowledge_bases': [kb],
        'skills_selection_mode': 'none', 'mcp_selection_mode': 'none'}})['data']['id']
    for space in spaces:
        for kind, rid in [('knowledge_base', kb), ('agent', agent)]:
            owner.call('POST', f'/spaces/{space}/resources', {
                'kind': kind, 'id': rid, 'source_tenant_id': tenant, 'allow_edit': False, 'allow_download': False})
    path = '/spaces/admin/users/' + uid + '/handover'
    preview = admin.call('GET', path)['data']
    assert preview['is_active'] and preview['user_id'] == uid
    assert {s['id'] for s in preview['spaces']} == set(spaces)
    assert len(preview['resources']) == 4 and {r['resource_id'] for r in preview['resources']} == {kb, agent}
    assert all(r['owner_user_id'] == uid for r in preview['resources'])
    expect(successor, 403, 'GET', path)
    expect(outsider, 403, 'GET', path)
    expect(admin, 400, 'POST', '/spaces/admin/handovers/' + spaces[0], {'user_id': sid, 'owner_user_id': uid})
    assert admin.call('POST', '/system/admin/users/status', {'user_id': uid, 'is_active': False})['is_active'] is False
    expect(owner, 401, 'GET', '/spaces')
    preview = admin.call('GET', path)['data']
    assert preview['is_active'] is False
    expect(admin, 409, 'POST', '/spaces/admin/handovers/' + spaces[0], {'user_id': sid, 'owner_user_id': users[2]['id']})
    handover = {'kind': 'knowledge_base', 'resource_id': kb, 'source_tenant_id': tenant, 'space_id': unrelated, 'owner_user_id': uid}
    expect(admin, 403, 'POST', '/spaces/admin/resource-handovers', handover)
    expect(successor, 403, 'POST', '/spaces/admin/handovers/' + spaces[0], {'user_id': sid, 'owner_user_id': uid})
    admin.call('POST', '/spaces/admin/handovers/' + spaces[0], {'user_id': sid, 'owner_user_id': uid})
    assert len(admin.call('GET', path)['data']['spaces']) == 1
    admin.call('POST', '/spaces/admin/handovers/' + spaces[1], {'user_id': sid, 'owner_user_id': uid})
    for kind, rid in [('knowledge_base', kb), ('agent', agent)]:
        admin.call('POST', '/spaces/admin/resource-handovers', {
            'kind': kind, 'resource_id': rid, 'source_tenant_id': tenant, 'space_id': spaces[0], 'owner_user_id': uid})
    preview = admin.call('GET', path)['data']
    assert not preview['spaces'] and not preview['resources']
    second = successor.call('GET', f'/spaces/{spaces[1]}/resources')['data']
    assert {r['id'] for r in second} == {kb, agent}
    assert all(not r['grant']['allow_edit'] and not r['grant']['allow_download'] for r in second)
    expect(outsider, 403, 'GET', '/spaces/' + spaces[0] + '/members')
    expect(successor, (403, 404), 'GET', '/knowledge-bases/' + private)
    # Existing member-management transfer remains available to an active owner.
    invite = successor.call('POST', '/spaces/' + spaces[0] + '/invitations', {'email': identities[2]['email']})['data']['id']
    outsider.call('POST', '/spaces/invitations/' + invite + '/accept')
    successor.call('POST', '/spaces/' + spaces[0] + '/transfer', {'user_id': users[2]['id']})
    members = outsider.call('GET', '/spaces/' + spaces[0] + '/members')['data']
    assert next(m for m in members if m['user_id'] == users[2]['id'])['role'] == 'owner'
    result = {'passed': True, 'account_scoped_preview': True, 'ordinary_denied': True,
              'disable_revokes_sessions': True, 'resume_partial_handover': True,
              'stale_owner_rejected': True, 'only_existing_share_destinations': True,
              'knowledge_and_agent_handover': True, 'other_grants_retained': True,
              'private_content_excluded': True, 'member_transfer_retained': True}
    save_json(state/'handover-acceptance.json', result)
    return result
