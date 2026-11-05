#!/usr/bin/env python3
"""Boot isolated application services against an already migrated DB copy."""
import argparse
import json
from pathlib import Path
from docker_control import APP, FRONTEND, clone, exists, action, wait_health, inspect, sql, root_helper
from api_client import private_write

DB = 'solex_user_spaces_rehearsal_20261105'
STATE = Path('/data/solexai/yongjun/build-cache/user-spaces-rehearsal-20261105')
PREFIX = 'solex-spaces-rehearsal'


def run(release):
    meta = json.loads((release / 'release.json').read_text())
    if sql('SELECT count(*) FROM solex_space_migration_state WHERE id=1 AND completed_at IS NOT NULL', DB) != '1':
        raise RuntimeError('Complete the isolated data migration first')
    if exists(PREFIX + '-app'):
        raise RuntimeError('Rehearsal already exists; restart only its candidate container explicitly')
    STATE.mkdir(mode=0o700, exist_ok=True)
    # A separate copy of files, redis, parser outputs and execution bindings.
    root_helper("""import pathlib,shutil,os
p=pathlib.Path('/state/data');p.mkdir(exist_ok=True)
if not (p/'files').exists():shutil.copytree('/deployment/data/files',p/'files')
(p/'docreader').mkdir(exist_ok=True)
(p/'redis').mkdir(exist_ok=True)
shutil.copyfile('/deployment/admin.json','/state/admin.json')
os.chmod('/state/admin.json',0o600)
st=os.stat('/state');os.chown('/state/admin.json',st.st_uid,st.st_gid)
""", STATE)
    sql("UPDATE automations SET enabled=FALSE; DELETE FROM automation_runs; DELETE FROM session_sandbox_assignments; DELETE FROM sandbox_user_machines; UPDATE schema_migrations SET version=137,dirty=FALSE;", DB)
    # Isolated Redis does not receive any production queued/running work.
    for service in ('redis', 'docreader'):
        old = inspect('weknora-native-' + service + '-1')
        binds = [str(STATE / 'data' / service) + ':' + ('/data' if service == 'redis' else '/tmp/docreader')]
        clone(old, PREFIX + '-' + service, binds=binds, ports={}, isolated=True)
        action(PREFIX + '-' + service, 'start')
        wait_health(PREFIX + '-' + service)
    old = inspect(APP)
    changes = {'DB_NAME': DB, 'SOLEX_USER_SPACES': 'true', 'REDIS_ADDR': PREFIX + '-redis:6379',
               'DOCREADER_ADDR': PREFIX + '-docreader:50051', 'SOLEX_SANDBOX_INSTANCE': PREFIX,
               'FRONTEND_BASE_URL': 'http://127.0.0.1:18085'}
    binds = [str(STATE / 'data/files') + ':/data/files', str(STATE / 'data/docreader') + ':/tmp/docreader:ro', '/var/run/docker.sock:/var/run/docker.sock']
    clone(old, PREFIX + '-app', meta['app_image'], changes, binds, {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '18084'}]}, True)
    action(PREFIX + '-app', 'start'); wait_health(PREFIX + '-app')
    clone(inspect(FRONTEND), PREFIX + '-frontend', meta['ui_image'], {'APP_HOST': PREFIX + '-app'}, [], {'80/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '18085'}]}, True)
    action(PREFIX + '-frontend', 'start'); wait_health(PREFIX + '-frontend')
    private_write(STATE / 'candidate.json', json.dumps({'revision': meta['source_revision'], 'api': 'http://127.0.0.1:18084', 'ui': 'http://127.0.0.1:18085'}))
    print('Isolated candidate is ready; production services are unchanged')


if __name__ == '__main__':
    p = argparse.ArgumentParser();p.add_argument('--release', required=True)
    run(Path(p.parse_args().release))
