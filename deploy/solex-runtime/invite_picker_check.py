"""Account-picker acceptance against an isolated database; no model calls."""
import secrets
from urllib.parse import urlencode
from api_client import Client, save_json
from account_handover_check import expect


def run(base, admin_credentials, state):
    if base != 'http://127.0.0.1:18084':
        raise ValueError('Invitation mutations require the isolated candidate')
    admin = Client(base)
    admin.login(admin_credentials)
    clients, users, identities = [], [], []
    for label in ('owner', 'member', 'disabled', 'guest', 'candidate'):
        identity = {'username': 'invite_' + label + '_' + secrets.token_hex(2),
                    'email': 'invite-' + secrets.token_hex(5) + '@example.invalid',
                    'password': secrets.token_urlsafe(20) + 'Aa1!'}
        client = Client(base)
        users.append(client.call('POST', '/auth/register', identity)['user'])
        client.login(identity)
        clients.append(client)
        identities.append(identity)
    owner, member, disabled, guest, candidate = clients
    space = owner.call('POST', '/spaces', {'name': '用户列表邀请验收'})['data']['id']
    path = '/spaces/' + space

    def candidates(client=owner, **query):
        return client.call('GET', path + '/invite-candidates?' + urlencode(query))['data']

    page = candidates()
    assert users[0]['id'] not in {u['id'] for u in page['items']}
    assert all(set(u) == {'id', 'username', 'email'} for u in page['items'])
    for query in (identities[1]['username'].upper(), identities[1]['email'].upper()):
        rows = candidates(q=query)['items']
        assert [u['id'] for u in rows] == [users[1]['id']]
    first, second = candidates(limit=1), candidates(limit=1, offset=1)
    assert first['has_more'] and first['items'][0]['id'] != second['items'][0]['id']
    assert not candidates(q="' OR 1=1 --")['items']
    expect(Client(base), 401, 'GET', path + '/invite-candidates')
    expect(guest, 403, 'GET', path + '/invite-candidates')
    expect(admin, 403, 'GET', path + '/invite-candidates')
    for query in ('offset=-1', 'limit=0', 'limit=51', 'offset=abc'):
        expect(owner, 400, 'GET', path + '/invite-candidates?' + query)
    expect(owner, 400, 'POST', path + '/invitations', {'user_id': users[1]['id'], 'email': identities[1]['email']})
    expect(owner, 409, 'POST', path + '/invitations', {'user_id': users[0]['id']})
    invite = owner.call('POST', path + '/invitations', {'user_id': users[1]['id'], 'role': 'owner'})['data']
    assert invite['email'] == identities[1]['email']
    assert not candidates(q=identities[1]['email'])['items']
    expect(owner, 409, 'POST', path + '/invitations', {'user_id': users[1]['id']})
    expect(guest, 404, 'POST', '/spaces/invitations/' + invite['id'] + '/accept')
    member.call('POST', '/spaces/invitations/' + invite['id'] + '/accept')
    rows = member.call('GET', path + '/members')['data']
    assert next(u for u in rows if u['user_id'] == users[1]['id'])['role'] == 'viewer'
    assert not candidates(q=identities[1]['email'])['items']
    expect(member, 403, 'GET', path + '/invite-candidates')
    expect(member, 403, 'POST', path + '/invitations', {'user_id': users[3]['id']})
    owner.call('PUT', path + '/members/' + users[1]['id'], {'role': 'editor'})
    expect(member, 403, 'GET', path + '/invite-candidates')
    owner.call('PUT', path + '/members/' + users[1]['id'], {'role': 'admin'})
    assert candidates(member, q=identities[3]['email'])['items'][0]['id'] == users[3]['id']
    invite = member.call('POST', path + '/invitations', {'user_id': users[3]['id']})['data']
    # Revocation makes the account selectable again; email API remains compatible.
    member.call('DELETE', '/spaces/invitations/' + invite['id'])
    assert candidates(q=identities[3]['email'])['items']
    invite = owner.call('POST', path + '/invitations', {'email': identities[3]['email']})['data']
    guest.call('POST', '/spaces/invitations/' + invite['id'] + '/accept')
    admin.call('POST', '/system/admin/users/status', {'user_id': users[2]['id'], 'is_active': False})
    assert not candidates(q=identities[2]['email'])['items']
    expect(owner, 409, 'POST', path + '/invitations', {'user_id': users[2]['id']})
    owner.call('PUT', path + '/archive', {'archived': True})
    expect(owner, 409, 'GET', path + '/invite-candidates')
    expect(owner, 409, 'POST', path + '/invitations', {'user_id': users[4]['id']})
    owner.call('PUT', path + '/archive', {'archived': False})
    # Keep one synthetic selectable account for browser verification before cleanup.
    save_json(state / 'invite-fixtures.json', {'users': users, 'identities': identities, 'space': space})
    result = {'passed': True, 'manager_scoped_directory': True, 'minimal_identity_fields': True,
              'search_and_pagination': True, 'inactive_member_pending_filters': True,
              'stale_selection_rejected': True, 'viewer_on_acceptance': True,
              'ordinary_space_admin_can_invite': True, 'revoke_and_legacy_email_compatible': True,
              'archived_space_rejected': True}
    save_json(state / 'invite-acceptance.json', result)
    return result
