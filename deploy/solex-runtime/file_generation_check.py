"""Real ordinary-user attachment -> Python -> XLSX -> authenticated download check."""
import io
import json
import secrets
import time
from urllib import error, request
import xml.etree.ElementTree as ET
import zipfile
from api_client import ApiError
from file_generation_setup import SETTINGS, SETTING


def binary_download(client, path):
    req = request.Request(client.base + '/api/v1' + path,
                          headers={'Authorization': 'Bearer ' + client.token})
    try:
        with client.opener.open(req, timeout=30) as response:
            return response.read(10 * 1024 * 1024)
    except error.HTTPError as exc:
        raise ApiError('GET', path, exc.code) from None


def workbook_values(content):
    ns = {'s': 'http://schemas.openxmlformats.org/spreadsheetml/2006/main'}
    with zipfile.ZipFile(io.BytesIO(content)) as archive:
        shared = []
        if 'xl/sharedStrings.xml' in archive.namelist():
            shared = [''.join(node.itertext()) for node in ET.fromstring(archive.read('xl/sharedStrings.xml'))]
        values = []
        for name in archive.namelist():
            if not name.startswith('xl/worksheets/sheet') or not name.endswith('.xml'):
                continue
            for cell in ET.fromstring(archive.read(name)).findall('.//s:c', ns):
                text = ''.join(cell.find('s:is', ns).itertext()) if cell.get('t') == 'inlineStr' else cell.findtext('s:v', '', ns)
                values.append(shared[int(text)] if cell.get('t') == 's' else text)
        return values


def check_file_generation(admin, ordinary):
    rows = {row['key']: row for row in admin.call('GET', SETTINGS)}
    selected = rows[SETTING]['value']
    if not selected:
        return {'enabled': False, 'skipped': 'administrator_disabled'}
    ordinary_configs = ordinary.call('GET', '/sandbox-configs')['data']
    if any(item['id'] == selected for item in ordinary_configs):
        raise RuntimeError('Ordinary user can inspect the central sandbox configuration')
    for method, path, payload in (
        ('POST', '/sandbox-configs', {}),
        ('PUT', SETTINGS + '/' + SETTING, {'value': ''}),
    ):
        try:
            ordinary.call(method, path, payload)
        except ApiError as exc:
            if exc.status != 403: raise
        else:
            raise RuntimeError('Ordinary user can modify or inspect execution infrastructure')

    # Reject stale/foreign selections without disturbing the valid default.
    try:
        admin.call('PUT', SETTINGS + '/' + SETTING, {'value': '00000000-0000-0000-0000-000000000001'})
    except ApiError as exc:
        if exc.status != 400: raise
    else:
        admin.call('PUT', SETTINGS + '/' + SETTING, {'value': selected})
        raise RuntimeError('File generation accepted a missing sandbox')

    agent_id = session_id = message_id = None
    complete = False
    try:
        agent_id = ordinary.call('POST', '/agents', {'name': '__file_generation_acceptance__',
            'config': {'agent_mode': 'smart-reasoning', 'kb_selection_mode': 'none',
                       'skills_selection_mode': 'none', 'mcp_selection_mode': 'none',
                       'allowed_tools': [], 'system_prompt': '你是文件生成助手。使用实际执行工具完成用户要求，不委派任务。'}})['data']['id']
        session_id = ordinary.call('POST', '/sessions', {'title': '__file_generation_acceptance__', 'agent_id': agent_id})['data']['id']
        marker = 'fixture-' + secrets.token_hex(8)
        fixture = ('name,quantity\n' + marker + ',17\nsecond,25\n').encode()
        upload = ordinary.call('POST', '/sessions/' + session_id + '/attachments', upload=('input.csv', fixture))['data']
        payload = {'query': '请读取附件 input.csv，实际调用 Python openpyxl 生成 /workspace/output/acceptance.xlsx。'
                           '工作表第一行是原始表头，之后保留两条原始记录，最后增加 total 行，quantity 为数量总和（写入数字值）。'
                           '保留原始 name，不要编造；将表头加粗。生成后重新用 openpyxl 打开验证，再提供文件。不要委派或询问。',
                   'agent_id': agent_id, 'agent_enabled': True, 'attachment_ids': [upload['id']],
                   'web_search_enabled': False, 'disable_title': True}
        req = request.Request(ordinary.base + '/api/v1/agent-chat/' + session_id,
                              data=json.dumps(payload).encode(), method='POST',
                              headers={'Authorization': 'Bearer ' + ordinary.token,
                                       'Content-Type': 'application/json', 'Accept': 'text/event-stream'})
        tools = []
        deadline = time.monotonic() + 240
        with ordinary.opener.open(req, timeout=90) as stream:
            for raw in stream:
                if time.monotonic() > deadline:
                    raise RuntimeError('File generation exceeded acceptance deadline')
                line = raw.decode().strip()
                if not line.startswith('data:') or line[5:].strip() in ('', '[DONE]'):
                    continue
                frame = json.loads(line[5:])
                data = frame.get('data') or {}
                message_id = frame.get('assistant_message_id') or data.get('assistant_message_id') or message_id
                kind = frame.get('response_type')
                if kind == 'error':
                    raise RuntimeError('File generation stream reported an error; contents omitted')
                if kind == 'tool_call':
                    tools.append(data.get('tool_name'))
                if kind in ('agent_prompt_required', 'tool_approval_required'):
                    raise RuntimeError('Synthetic file generation unexpectedly requires manual input')
                if kind == 'complete':
                    complete = True
                    break
        if not complete or 'shell_exec' not in tools:
            raise RuntimeError(f'File generation did not execute a shell and complete (complete={complete}, tool_calls={len(tools)})')
        artifacts = []
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            artifacts = ordinary.call('GET', f'/sessions/{session_id}/messages/{message_id}/artifacts')['data']
            if any(a.get('file_name') == 'acceptance.xlsx' for a in artifacts): break
            time.sleep(1)
        artifact = next((a for a in artifacts if a.get('file_name') == 'acceptance.xlsx'), None)
        if not artifact:
            raise RuntimeError('No downloadable XLSX artifact was persisted')
        path = f'/sessions/{session_id}/messages/{message_id}/artifacts/{artifact["index"]}/download'
        content = binary_download(ordinary, path)
        values = workbook_values(content)
        if not {marker, '17', '25', '42'}.issubset(values):
            raise RuntimeError('Workbook does not contain the actual attachment values and total')
        # An unrelated workspace, including its administrator, cannot download
        # a user's private session file through the artifact endpoint.
        try:
            binary_download(admin, path)
        except ApiError as exc:
            if exc.status not in (403, 404): raise
        else:
            raise RuntimeError('Another workspace downloaded a private session artifact')
        return {'passed': True, 'skills_disabled': True, 'model_unspecified': True,
                'attachment_values_verified': True, 'xlsx_download_bytes': len(content),
                'python_execution': True, 'cross_workspace_download_denied': True,
                'ordinary_configuration_denied': True, 'tool_calls': len(tools)}
    finally:
        if session_id:
            if not complete and message_id:
                try: ordinary.call('POST', '/sessions/' + session_id + '/stop', {'message_id': message_id})
                except Exception: pass
            ordinary.call('DELETE', '/sessions/' + session_id)
        if agent_id:
            ordinary.call('DELETE', '/agents/' + agent_id)
