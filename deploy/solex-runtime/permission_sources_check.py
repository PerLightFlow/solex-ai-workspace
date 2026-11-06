"""Check source aggregation through real APIs in an isolated database only."""
import secrets

from api_client import Client, save_json
from account_handover_check import expect


def run(base, admin_credentials, state):
    if base != 'http://127.0.0.1:18084':
        raise ValueError('Permission-source mutations require the isolated candidate')
    clients, users = [], []
    for label in ('owner', 'editor'):
        identity = {'username': 'sources_' + label + '_' + secrets.token_hex(2),
                    'email': 'sources-' + secrets.token_hex(5) + '@example.invalid',
                    'password': secrets.token_urlsafe(20) + 'Aa1!'}
        client = Client(base)
        users.append(client.call('POST', '/auth/register', identity)['user'])
        client.login(identity)
        clients.append(client)
    owner, member = clients
    spaces = [owner.call('POST', '/spaces', {'name': '来源合并验收'})['data']['id'] for _ in range(2)]
    for space in spaces:
        invitation = owner.call('POST', f'/spaces/{space}/invitations', {'user_id': users[1]['id']})['data']['id']
        member.call('POST', '/spaces/invitations/' + invitation + '/accept')
    owner.call('PUT', f'/spaces/{spaces[0]}/members/{users[1]["id"]}', {'role': 'editor'})
    kb = owner.call('POST', '/knowledge-bases', {'name': '合成来源资料', 'type': 'document'})['data']['id']
    agent = owner.call('POST', '/agents', {'name': '合成来源助手', 'config': {
        'agent_mode': 'smart-reasoning', 'kb_selection_mode': 'none',
        'skills_selection_mode': 'none', 'mcp_selection_mode': 'none'}})['data']['id']
    resources = [{'kind': kind, 'id': rid, 'source_tenant_id': users[0]['tenant_id']}
                 for kind, rid in [('knowledge_base', kb), ('agent', agent)]]

    def listing(space):
        return member.call('GET', f'/spaces/{space}/resources')['data']

    def flags(access):
        return tuple(access[key] for key in ('read', 'edit', 'download', 'manage'))

    for resource in resources:
        # A restrictive retained grant must not replace ownership capabilities.
        for space in spaces:
            owner.call('POST', f'/spaces/{space}/resources', {
                **resource, 'allow_edit': False, 'allow_download': False})
        owner.call('POST', f'/spaces/{spaces[0]}/resources/transfer', resource)
    for row in listing(spaces[0]):
        assert row['owned']
        assert flags(row['access']) == (True, True, True, False)
        sources = {source['space_id']: source for source in row['access']['sources']}
        assert len(row['access']['sources']) == len(sources) == 2
        assert sources[spaces[0]]['role'] == 'editor'
        assert sources[spaces[0]]['allow_edit'] and sources[spaces[0]]['allow_download']
        assert sources[spaces[1]]['role'] == 'viewer'
        assert not sources[spaces[1]]['allow_edit'] and not sources[spaces[1]]['allow_download']
    assert len(listing(spaces[0])) == len(listing(spaces[1])) == 2
    assert all(not row['owned'] for row in listing(spaces[1]))
    expect(Client(base), 401, 'GET', f'/spaces/{spaces[0]}/resources')
    document = member.call('POST', f'/knowledge-bases/{kb}/knowledge/manual', {
        'title': '合成草稿', 'content': '仅用于权限验收，不调用模型。', 'status': 'draft'})['data']['id']
    owner.call('PUT', f'/spaces/{spaces[0]}/archive', {'archived': True})
    assert all(flags(row['access']) == (True, False, True, False) for row in listing(spaces[0]))
    expect(member, 403, 'POST', f'/knowledge-bases/{kb}/knowledge/manual', {
        'title': '拒绝写入', 'content': 'deny', 'status': 'draft'})
    owner.call('PUT', f'/spaces/{spaces[0]}/archive', {'archived': False})
    # Revoking a real path still removes its powers and its source explanation.
    owner.call('DELETE', f'/spaces/{spaces[0]}/members/{users[1]["id"]}')
    for row in listing(spaces[1]):
        assert flags(row['access']) == (True, False, False, False)
        assert [source['space_id'] for source in row['access']['sources']] == [spaces[1]]
    member.call('GET', '/knowledge/' + document)
    for resource in resources:
        owner.call('DELETE', f'/spaces/{spaces[1]}/resources', resource)
    assert listing(spaces[1]) == []
    expect(member, (403, 404), 'GET', '/knowledge/' + document)
    result = {'passed': True, 'knowledge_and_agent_sources_unique': True,
              'different_space_ids_preserved': True, 'ownership_permission_union_preserved': True,
              'archive_and_revocation_preserved': True, 'ordinary_editor_write_verified': True,
              'model_calls': 0}
    save_json(state / 'permission-sources-acceptance.json', result)
    return result
