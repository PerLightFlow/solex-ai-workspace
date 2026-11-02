"""Managed-runtime live checks using one disposable personal workspace."""
import json
import secrets
import time
from api_client import ROOT, Client, ApiError, save_json
from stream_check import check_stream


def expect_forbidden(client, method, path, data=None):
    try:
        client.call(method, path, data)
    except ApiError as exc:
        if exc.status == 403:
            return
        raise
    raise RuntimeError('A managed configuration write was not rejected')


def expect_not_found(client, method, path, data=None):
    try:
        client.call(method, path, data)
    except ApiError as exc:
        if exc.status == 404:
            return
        raise
    raise RuntimeError('A removed system agent was still accessible')


def check_builtin_catalog(admin, ordinary, private_kb_id):
    """Only mutate a synthetic system agent; never remove a shipped definition."""
    system_id = None
    session_id = None
    deleted = False
    payload = {'name': '__system_agent_acceptance__', 'description': 'Synthetic global catalog check',
               'config': {'agent_mode': 'quick-answer', 'system_prompt': '请简短回答用户的问题。',
                          'kb_selection_mode': 'none'}}
    try:
        expect_forbidden(ordinary, 'GET', '/system/builtin-agents')
        expect_forbidden(ordinary, 'POST', '/system/builtin-agents', payload)
        definition = admin.call('POST', '/system/builtin-agents', payload)['data']
        system_id = definition['id']
        path = '/system/builtin-agents/' + system_id
        for method in ('PUT', 'DELETE'):
            expect_forbidden(ordinary, method, path, payload if method == 'PUT' else None)
        for method in ('PUT', 'DELETE'):
            expect_forbidden(ordinary, method, '/agents/' + system_id, payload if method == 'PUT' else None)
        ordinary_definition = ordinary.call('GET', '/agents/' + system_id)['data']
        if not ordinary_definition.get('is_builtin') or ordinary_definition.get('name') != payload['name']:
            raise RuntimeError('System agent was not inherited by an ordinary workspace')
        listing = ordinary.call('GET', '/agents')['data']
        if not any(a['id'] == system_id for a in listing):
            raise RuntimeError('System agent missing from ordinary agent catalog')
        revised = {**payload, 'name': '__system_agent_acceptance_updated__'}
        admin.call('PUT', path, revised)
        if ordinary.call('GET', '/agents/' + system_id)['data']['name'] != revised['name']:
            raise RuntimeError('System agent edit did not propagate across workspaces')
        # An administrator must not publish a private knowledge binding into
        # a global definition. User-selected KBs are still checked per caller.
        try:
            admin.call('PUT', path, {**revised, 'config': {**payload['config'],
                'kb_selection_mode': 'selected', 'knowledge_bases': [private_kb_id]}})
        except ApiError as exc:
            if exc.status not in (400, 403): raise
        else:
            raise RuntimeError('System catalog accepted a private knowledge binding')
        stream = check_stream(ordinary, agent_id=system_id)
        admin_stream = check_stream(admin, agent_id=system_id)
        session_id = ordinary.call('POST', '/sessions', {'title': '__removed_agent_acceptance__',
            'agent_id': system_id})['data']['id']
        admin.call('DELETE', path)
        deleted = True
        # Repeated reads also exercise lazy initialization: a removed item
        # must not be recreated from a default or stale workspace record.
        for client in (ordinary, admin, ordinary):
            if any(a['id'] == system_id for a in client.call('GET', '/agents')['data']):
                raise RuntimeError('Removed system agent reappeared in the catalog')
            expect_not_found(client, 'GET', '/agents/' + system_id)
        ordinary.call('GET', '/sessions/' + session_id)
        for endpoint in ('/knowledge-chat/', '/agent-chat/'):
            expect_not_found(ordinary, 'POST', endpoint + session_id,
                {'query': 'This removed agent must not run.', 'agent_id': system_id})
        return {'passed': True, 'admin_only_management': True, 'cross_workspace_create_update': True,
                'private_knowledge_binding_rejected': True, 'removed_agent_unavailable': True,
                'global_agent_chat_stream': stream, 'admin_global_agent_chat_stream': admin_stream}
    finally:
        cleanup_errors = []
        if session_id:
            try: ordinary.call('DELETE', '/sessions/' + session_id)
            except Exception: cleanup_errors.append('test-session')
        if system_id and not deleted:
            try: admin.call('DELETE', '/system/builtin-agents/' + system_id)
            except Exception: cleanup_errors.append('test-system-agent')
        if cleanup_errors:
            raise RuntimeError('System catalog acceptance cleanup failed: ' + ','.join(cleanup_errors))


def acceptance_identity():
    # Match the application's complex-password policy (8-32 characters).
    return {'username': '__managed_check_' + secrets.token_hex(4),
            'email': 'managed-' + secrets.token_hex(6) + '@example.invalid',
            'password': secrets.token_urlsafe(18) + 'Aa1!'}


def run(state_dir):
    admin = Client()
    admin.login(json.loads((ROOT / 'admin.json').read_text()))
    caps = admin.call('GET', '/system/capabilities')['data']
    if not caps.get('managed_runtime'):
        raise RuntimeError('Managed runtime capability is not active')
    state = acceptance_identity()
    client = Client()
    user_id = None
    tenant_id = None
    kb_id = None
    agent_id = None
    try:
        user = client.call('POST', '/auth/register', state)['user']
        user_id = user['id']
        login = client.login(state)
        tenant_id = login['user'].get('tenant_id') or user.get('tenant_id')
        if login['user'].get('is_system_admin'):
            raise RuntimeError('Acceptance requires an ordinary user')
        models = client.call('GET', '/models')['data']
        if not models or any(not m.get('is_builtin') for m in models):
            raise RuntimeError('Ordinary model catalog must contain shared models only')
        providers = client.call('GET', '/web-search-providers')['data']
        if not providers: raise RuntimeError('System web search provider was not inherited')
        defaults = client.call('GET', '/tenants/kv/default-settings')['data']
        if not defaults.get('chat_model_id') or not defaults.get('embedding_model_id'):
            raise RuntimeError('Ordinary user did not inherit configured defaults')
        for path in ['/models', '/mcp-services', '/web-search-providers', '/system/sandbox-check', '/system/parser-engines/check']:
            expect_forbidden(client, 'POST', path, {})
        expect_forbidden(client, 'PUT', '/tenants/kv/default-settings', {})
        expect_forbidden(client, 'PUT', '/models/' + defaults['chat_model_id'], {})
        # No model ID, endpoint or credential in either request.
        agent = client.call('POST', '/agents', {'name': '__managed_acceptance__',
            'description': 'Synthetic acceptance only', 'config': {'agent_mode':'quick-answer',
            'system_prompt':'只需简短回答用户的问题。', 'kb_selection_mode':'none'}})['data']
        agent_id = agent['id']
        kb = client.call('POST', '/knowledge-bases', {'name':'__managed_acceptance__',
            'description':'Synthetic acceptance only', 'type':'document', 'wiki_config':{'enabled':False}, 'profile_config':{'enabled':False}, 'auto_tag_config':{'enabled':False}})['data']
        kb_id = kb['id']
        if kb.get('embedding_model_id') != defaults['embedding_model_id']:
            raise RuntimeError('New knowledge base did not inherit embedding default')
        if kb.get('summary_model_id') != defaults['chat_model_id']:
            raise RuntimeError('New knowledge base did not inherit generation default')
        document = client.call('POST', '/knowledge-bases/' + kb_id + '/knowledge/file',
            upload=('managed-acceptance.md', '# 合成验收资料\n\n测试项目使用 ESP32-P4 芯片，测试甲负责采集电路。\n'.encode()))['data']
        doc_id = document['id']
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            parsed = client.call('GET', '/knowledge/' + doc_id)['data']
            if parsed.get('parse_status') == 'failed': raise RuntimeError('Ordinary document indexing failed')
            if parsed.get('parse_status') == 'completed': break
            time.sleep(2)
        else: raise RuntimeError('Ordinary document indexing timed out')
        hits = client.call('POST', '/knowledge-search', {'query':'ESP32-P4 采集电路',
            'knowledge_ids':[doc_id], 'match_count':5, 'disable_vector_match':False,
            'disable_keywords_match':True, 'rerank':{'enabled':False}})['data']
        if not hits or not any('ESP32-P4' in hit.get('content','') for hit in hits):
            raise RuntimeError('Ordinary shared-embedding vector retrieval failed')
        # Shared runtime must not imply shared private agents or knowledge bases.
        other_agents = admin.call('GET', '/agents')['data']
        if isinstance(other_agents, dict):
            other_agents = other_agents.get('items', other_agents.get('agents', []))
        if any(a.get('id') == agent_id for a in other_agents):
            raise RuntimeError('Personal agent appeared in the admin workspace')
        other_kbs = admin.call('GET', '/knowledge-bases')['data']
        if any(k.get('id') == kb_id for k in other_kbs):
            raise RuntimeError('Personal knowledge base appeared in the admin workspace')
        chat = check_stream(client, agent_id=agent_id)
        catalog = check_builtin_catalog(admin, client, kb_id)
        result = {'passed': True, 'time': time.time(), 'ordinary_registration': True,
            'default_models_inherited': True, 'search_provider_inherited': True, 'configuration_writes_forbidden': True,
            'create_agent_without_model': True, 'create_kb_without_model': True, 'ordinary_upload_and_vector_retrieval':True,
            'personal_workspace_list_isolation': True, 'chat_stream': chat, 'system_agent_catalog': catalog,
            'capacity_tested': False}
        save_json(state_dir / 'acceptance.json', result)
        return result
    finally:
        cleanup_errors = []
        for path in ([('/knowledge-bases/' + kb_id)] if kb_id else []) + ([('/agents/' + agent_id)] if agent_id else []):
            try: client.call('DELETE', path)
            except Exception: cleanup_errors.append(path.split('/')[1])
        if user_id:
            try: admin.call('POST', '/system/admin/users/status', {'user_id':user_id, 'is_active':False})
            except Exception: cleanup_errors.append('test-account-disable')
        save_json(state_dir / 'test-identity.json', {'user_id':user_id, 'tenant_id':tenant_id,
            'test_account_disabled':user_id is not None and 'test-account-disable' not in cleanup_errors,
            'cleanup_errors':cleanup_errors})
        if cleanup_errors:
            raise RuntimeError('Synthetic acceptance cleanup failed; see protected receipt')
