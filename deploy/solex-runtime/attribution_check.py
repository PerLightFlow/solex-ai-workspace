"""File attribution and shared-reader acceptance on disposable resources only."""
import secrets

from api_client import Client, ApiError


def denied(client, method, path, body=None):
    try:
        client.call(method, path, body)
    except ApiError as exc:
        if exc.status in (403, 404):
            return
        raise
    raise RuntimeError('An unauthorized document request was accepted')


def check_document_attribution(admin, owner, identity, owner_id, tenant_id, kb_id, initial):
    document_id = initial['id']
    path = '/knowledge/' + document_id
    keys = ('created_by', 'created_by_name', 'last_modified_by', 'last_modified_by_name', 'last_modified_at')
    if initial.get('created_by') != owner_id or initial.get('created_by_name') != identity['username']:
        raise RuntimeError('Upload attribution is missing or incorrect')
    after_parse = owner.call('GET', path)['data']
    if any(after_parse.get(key) != initial.get(key) for key in keys):
        raise RuntimeError('Background parsing changed user attribution')

    people, org_id, share_id = [], None, None
    try:
        for role in ('editor', 'reader'):
            state = {'username': '__attribution_' + role + '_' + secrets.token_hex(3),
                     'email': secrets.token_hex(8) + '@example.invalid',
                     'password': secrets.token_urlsafe(18) + 'Aa1!'}
            client = Client()
            user = client.call('POST', '/auth/register', state)['user']
            people.append((client, user, state))
            client.login(state)
        editor, editor_user, editor_state = people[0]
        reader, reader_user, _ = people[1]
        denied(reader, 'GET', path + '/activity')
        owner.call('POST', f'/tenants/{tenant_id}/members', {'email': editor_state['email'], 'role': 'admin'})
        editor.token = editor.call('POST', '/auth/switch-tenant', {'tenant_id': int(tenant_id)})['token']
        # The handler must ignore forged attribution in an otherwise valid edit.
        editor.call('PUT', path, {'title': '__attribution_renamed__',
            'created_by': editor_user['id'], 'created_by_name': 'forged',
            'last_modified_by': owner_id, 'last_modified_by_name': 'forged',
            'last_modified_at': '2000-01-01T00:00:00Z'})
        editor.call('PUT', path, {'description': 'Synthetic attribution acceptance summary',
                                 'custom_metadata': {'test': 'attribution'}})
        changed = owner.call('GET', path)['data']
        if changed.get('created_by') != owner_id or changed.get('created_by_name') != identity['username']:
            raise RuntimeError('An edit changed the original uploader')
        if changed.get('last_modified_by') != editor_user['id'] or changed.get('last_modified_by_name') != editor_state['username']:
            raise RuntimeError('The second user was not recorded as the last editor')
        if not changed.get('last_modified_at') or changed['last_modified_at'] in (initial.get('last_modified_at'), '2000-01-01T00:00:00Z'):
            raise RuntimeError('Human modification time is missing or client-controlled')

        org = owner.call('POST', '/organizations', {'name': '__attribution_acceptance__'})['data']
        org_id = org['id']
        owner.call('PUT', '/organizations/' + org_id, {'require_approval': False})
        code = org.get('invite_code')
        if not code:
            code = owner.call('POST', '/organizations/' + org_id + '/invite-code', {})['data']['invite_code']
        reader.call('POST', '/organizations/join', {'invite_code': code})
        share = owner.call('POST', '/knowledge-bases/' + kb_id + '/shares',
                           {'organization_id': org_id, 'permission': 'viewer'})['data']
        share_id = share['id']
        visible = reader.call('GET', path)['data']
        if visible.get('created_by_name') != identity['username'] or visible.get('last_modified_by_name') != editor_state['username']:
            raise RuntimeError('Shared readers cannot see attribution')
        denied(reader, 'PUT', path, {'title': 'not allowed'})
        denied(reader, 'GET', '/knowledge-bases/' + kb_id + '/activity')
        history = reader.call('GET', path + '/activity?limit=100')['data']
        fields = {'id', 'action', 'actor_name', 'actor_kind', 'created_at', 'changed_fields'}
        if any(set(entry) - fields for entry in history):
            raise RuntimeError('Shared history exposed fields outside its safe projection')
        if not any(entry['action'] == 'knowledge.created' and entry['actor_name'] == identity['username'] for entry in history):
            raise RuntimeError('Upload history is missing the uploader name')
        edits = [entry for entry in history if entry['action'] == 'knowledge.updated' and entry['actor_name'] == editor_state['username']]
        if len(edits) < 2 or not {'title', 'description', 'custom_metadata'}.issubset({field for entry in edits for field in entry['changed_fields']}):
            raise RuntimeError('Metadata edits are missing from document history')
        page = reader.call('GET', path + '/activity?limit=1')
        older = reader.call('GET', path + '/activity?limit=1&after_id=' + str(page['next_cursor']))
        if len(page['data']) != 1 or len(older['data']) != 1 or older['data'][0]['id'] >= page['data'][0]['id']:
            raise RuntimeError('Document history cursor pagination failed')
        owner.call('DELETE', '/knowledge-bases/' + kb_id + '/shares/' + share_id)
        share_id = None
        denied(reader, 'GET', path + '/activity')
        return {'passed': True, 'upload_identity': True, 'second_user_edit_identity': True,
                'background_preserves_attribution': True, 'client_spoofing_rejected': True,
                'shared_reader_safe_history': True, 'metadata_changes_logged': True,
                'cursor_pagination': True, 'revoked_reader_denied': True}
    finally:
        failures = []
        if share_id:
            try: owner.call('DELETE', '/knowledge-bases/' + kb_id + '/shares/' + share_id)
            except Exception: failures.append('share')
        if org_id:
            try: owner.call('DELETE', '/organizations/' + org_id)
            except Exception: failures.append('organization')
        for _, user, _ in people:
            try: admin.call('POST', '/system/admin/users/status', {'user_id': user['id'], 'is_active': False})
            except Exception: failures.append('account')
        if failures:
            raise RuntimeError('Attribution acceptance cleanup failed: ' + ','.join(failures))
