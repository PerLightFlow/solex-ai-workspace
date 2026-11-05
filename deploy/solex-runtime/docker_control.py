"""Small Docker Engine client for the approved host; never logs container Env."""
import copy
import http.client
import json
import socket
import subprocess
import time
from pathlib import Path
from urllib.parse import quote

APP = 'weknora-native-app-1'
FRONTEND = 'weknora-native-frontend-1'
POSTGRES = 'weknora-native-postgres-1'
ROOT = Path('/data/solexai/yongjun/weknora-native')
ALLOWED = Path('/data/solexai/yongjun')


class UnixConnection(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect('/var/run/docker.sock')


def docker(method, path, data=None):
    conn = UnixConnection('localhost', timeout=180)
    try:
        conn.request(method, '/v1.41' + path, body=None if data is None else json.dumps(data),
                     headers={'Content-Type': 'application/json'})
        response = conn.getresponse()
        payload = response.read()
        if response.status >= 400:
            # Engine error text can include secret environment values or args.
            raise RuntimeError(f'Docker {method} {path.split("?")[0]} returned {response.status}')
        return json.loads(payload) if payload else None
    finally:
        conn.close()


def inspect(name):
    return docker('GET', '/containers/' + quote(name, safe='') + '/json')


def exists(name):
    result = subprocess.run(['docker', 'container', 'inspect', name], capture_output=True)
    return result.returncode == 0


def action(name, verb):
    docker('POST', '/containers/' + quote(name, safe='') + '/' + verb)


def remove(name):
    docker('DELETE', '/containers/' + quote(name, safe='') + '?force=true')


def rename(name, target):
    docker('POST', '/containers/' + quote(name, safe='') + '/rename?name=' + quote(target, safe=''))


def clone(original, name, image=None, env=None, binds=None, ports=None, isolated=False):
    config = copy.deepcopy(original['Config'])
    host = copy.deepcopy(original['HostConfig'])
    config['Image'] = image or config['Image']
    config['Hostname'] = ''
    values = dict(item.split('=', 1) for item in config.get('Env', []))
    values.update(env or {})
    config['Env'] = [f'{key}={value}' for key, value in values.items()]
    if isolated:
        config['Labels'] = {'io.solex.test': 'user-spaces-rehearsal'}
        host['RestartPolicy'] = {'Name': 'no'}
    if binds is not None:
        host['Binds'] = binds
        host.pop('Mounts', None)
    if ports is not None:
        host['PortBindings'] = ports
    network = host['NetworkMode']
    config['HostConfig'] = host
    config['NetworkingConfig'] = {'EndpointsConfig': {network: {'Aliases': [name] if isolated else [name, *[a for a in original['NetworkSettings']['Networks'][network].get('Aliases', []) if a not in [original['Id'], original['Id'][:12]]]]}}}
    result = docker('POST', '/containers/create?name=' + quote(name, safe=''), config)
    return result['Id']


def wait_health(name, seconds=240):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        state = inspect(name)['State']
        if state.get('Running') and state.get('Health', {}).get('Status', 'healthy') == 'healthy':
            return
        if state.get('Status') in ('dead', 'exited'):
            raise RuntimeError(name + ' exited; inspect protected server log')
        time.sleep(2)
    raise RuntimeError(name + ' readiness timed out')


def sql(query, database='weknora'):
    if database not in ('weknora', 'solex_user_spaces_rehearsal_20261105'):
        raise ValueError('Unexpected database')
    result = subprocess.run(['docker', 'exec', '-i', POSTGRES, 'psql', '-X', '-U', 'weknora', '-d', database, '-At', '-v', 'ON_ERROR_STOP=1'], input=query, text=True, capture_output=True)
    if result.returncode:
        safe = [line for line in result.stderr.splitlines() if line.startswith(('ERROR:', 'HINT:'))]
        raise RuntimeError('Database operation failed: ' + ' | '.join(safe))
    return result.stdout.strip()


def root_helper(code, directory, readonly=True):
    directory = Path(directory).absolute()
    if not directory.is_relative_to(ALLOWED) or directory.is_symlink():
        raise ValueError('Helper destination outside approved directory')
    image = inspect(APP)['Config']['Image']
    result = subprocess.run(['docker', 'run', '--rm', '-i', '--network', 'none',
        '--entrypoint', '/usr/bin/python3', '-v', str(ROOT) + ':/deployment' + (':ro' if readonly else ''),
        '-v', str(directory) + ':/state', image, '-'], input=code, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError('Protected deployment file operation failed; no credentials logged')
    return result.stdout
