"""Deployment helpers. No secrets or provider response bodies are logged."""
import json
import mimetypes
import os
from pathlib import Path
import shlex
import time
from urllib import error, parse, request

ROOT = Path('/data/solexai/yongjun/weknora-native')
API = 'http://127.0.0.1:18083'



def private_write(path, text):
    path = Path(path)
    if path.is_symlink():
        raise ValueError('Refusing a symbolic link for deployment state')
    pending = path.with_name(path.name + '.pending')
    fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(text)
    pending.replace(path)


def save_json(path, data):
    private_write(path, json.dumps(data, ensure_ascii=False, indent=2) + '\n')


def read_env(path):
    """Parse data without evaluating shell commands or dotenv substitutions."""
    result = {}
    for line in Path(path).read_text().splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        key, sep, value = line.partition('=')
        if not sep:
            continue
        key = (key[7:] if key.startswith('export ') else key).strip()
        value = value.strip()
        if value.startswith(('"', "'")):
            words = shlex.split(value, comments=True)
            if len(words) != 1:
                raise ValueError('Unsupported quoted environment value')
            value = words[0]
        else:
            value = value.split(' #', 1)[0].rstrip()
        result[key] = value
    return result


def save_env(path, fields):
    # Only generated deployment values are emitted; model credentials use API calls.
    if any(any(c in str(v) for c in '\r\n\x00$\'"') for v in fields.values()):
        raise ValueError('Unsafe deployment environment value')
    private_write(path, ''.join(f'{k}={v}\n' for k, v in fields.items()))


class ApiError(RuntimeError):
    def __init__(self, method, path, status):
        self.status = status
        super().__init__(f'{method} {path.split("?")[0]} failed (HTTP {status}; response omitted)')


class Client:
    def __init__(self, base=API):
        if parse.urlsplit(base).hostname != '127.0.0.1':
            raise ValueError('Administration requires the loopback endpoint')
        self.base = base
        self.token = ''
        self.opener = request.build_opener(request.ProxyHandler({}))

    def call(self, method, path, data=None, form=False, upload=None):
        headers = {'Accept-Language': 'zh-CN',
                   'Content-Type': 'application/x-www-form-urlencoded' if form else 'application/json'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        body = None if data is None else (parse.urlencode(data).encode() if form else json.dumps(data).encode())
        if upload is not None:
            filename, content = upload
            if any(c in filename for c in '\r\n"'):
                raise ValueError('Invalid upload filename')
            boundary = 'WeKnoraNativeAcceptanceBoundary'
            headers['Content-Type'] = 'multipart/form-data; boundary=' + boundary
            mime = mimetypes.guess_type(filename)[0] or 'application/octet-stream'
            body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
                    f'Content-Type: {mime}\r\n\r\n').encode() + content + f'\r\n--{boundary}--\r\n'.encode()
        req = request.Request(self.base + '/api/v1' + path, data=body, headers=headers, method=method)
        try:
            with self.opener.open(req, timeout=180) as response:
                result = json.load(response)
        except error.HTTPError as exc:
            raise ApiError(method, path, exc.code) from None
        except (error.URLError, TimeoutError, ValueError):
            raise RuntimeError(f'{method} {path.split("?")[0]} transport/JSON failure; details omitted') from None
        if isinstance(result, dict) and result.get('success') is False:
            raise RuntimeError(f'{method} {path.split("?")[0]} unsuccessful; response omitted')
        return result

    def login(self, state):
        result = self.call('POST', '/auth/login', {k: state[k] for k in ('email', 'password')})
        self.token = result['token']
        return result


def wait_health(seconds=300):
    opener = request.build_opener(request.ProxyHandler({}))
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            with opener.open(API + '/health', timeout=5) as response:
                if response.status == 200:
                    return
        # A restarting server can accept TCP, then close before sending headers.
        # RemoteDisconnected / ConnectionResetError are OSError subclasses,
        # and urllib does not wrap every response-read failure in URLError.
        except OSError:
            pass
        time.sleep(2)
    raise RuntimeError('Native backend health check timed out')
