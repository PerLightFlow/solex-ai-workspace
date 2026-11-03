"""Provision the shared sandbox template; keep credentials in memory only."""
import json
from api_client import Client, ROOT, save_json

SETTING = 'agent.file_generation_sandbox_id'
SETTINGS = '/system/admin/settings'
IMAGE = 'solex/file-sandbox:20261103'
IMAGE_ID = 'sha256:a7d941038a47e9cd5e8605b8535fa8926b45ecc3c3cbb56333a7cee8ac5e9cb6'


def admin_client():
    client = Client()
    client.login(json.loads((ROOT / 'admin.json').read_text()))
    return client


def configure(state_dir):
    client = admin_client()
    rows = {r['key']: r for r in client.call('GET', SETTINGS)}
    state = {'previous': {key: rows[key] for key in (SETTING, 'sandbox.docker_enabled')},
             'created_config_id': None}
    save_json(state_dir / 'file-generation-before.json', state)
    # A later release preserves the administrator's chosen template.
    if rows[SETTING].get('value') or rows[SETTING].get('id'):
        return
    config = {'sandbox_type': 'docker', 'allocation_mode': 'session',
              'default_timeout_sec': 120, 'network': {'deny_egress_by_default': True},
              'docker': {'image': IMAGE, 'cpu_limit': 1, 'memory_limit_mb': 1024,
                         'pids_limit': 128, 'network_mode': 'none', 'idle_ttl_seconds': 1800}}
    client.call('PUT', SETTINGS + '/sandbox.docker_enabled', {'value': True})
    created = client.call('POST', '/sandbox-configs', {
        'name': 'SOLEX 文件生成', 'description': 'Python + openpyxl；按会话隔离，默认无网络访问。',
        'config': config})['data']
    state['created_config_id'] = created['id']
    save_json(state_dir / 'file-generation-before.json', state)
    check = client.call('POST', '/system/sandbox-check', {'config_id': created['id'], 'deep': True})['data']
    if not check.get('ok'):
        # Probe names are nonsecret, unlike provider messages.
        failed = [p['name'] for p in check.get('checks', []) if p.get('ok') is False]
        raise RuntimeError('File sandbox connectivity check failed: ' + ','.join(failed))
    client.call('PUT', SETTINGS + '/' + SETTING, {'value': created['id']})
    save_json(state_dir / 'file-generation-provisioned.json', {
        'config_id': created['id'], 'image': IMAGE, 'image_id': IMAGE_ID,
        'deep_check_passed': True, 'allocation_mode': 'session', 'network_mode': 'none'})


def rollback(state_dir):
    path = state_dir / 'file-generation-before.json'
    if not path.exists():
        return
    state = json.loads(path.read_text())
    client = admin_client()
    for key in (SETTING, 'sandbox.docker_enabled'):
        row = state['previous'][key]
        # A reset preserves whether a setting had an override before release.
        if row.get('id'):
            client.call('PUT', SETTINGS + '/' + key, {'value': row['value']})
        else:
            client.call('DELETE', SETTINGS + '/' + key)
    if state.get('created_config_id'):
        client.call('DELETE', '/sandbox-configs/' + state['created_config_id'])
