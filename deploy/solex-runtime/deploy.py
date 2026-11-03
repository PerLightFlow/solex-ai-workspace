#!/usr/bin/env python3
"""Deploy managed model configuration; backups and rollback stay server-side."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import time
from urllib import request
from api_client import ROOT, Client, private_write, save_json, wait_health, read_env
from acceptance import run as accept
import file_generation_setup

SOURCE = Path(__file__).resolve().parent


def command(*args, timeout=180, binary=False, input=None):
    result = subprocess.run(args, capture_output=True, text=not binary, timeout=timeout, input=input)
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed (exit {result.returncode}); credential-bearing output omitted')
    return result.stdout if binary else result.stdout.strip()


def sql(statement):
    return command('docker', 'exec', '-i', 'weknora-native-postgres-1', 'psql', '-v', 'ON_ERROR_STOP=1',
                   '-U', 'weknora', '-d', 'weknora', '-At', input=statement)


def replace_env(raw, changes):
    lines = raw.splitlines()
    for key, value in changes.items():
        if not re.fullmatch(r'[A-Z0-9_]+', key) or not re.fullmatch(r'[a-zA-Z0-9._/@:+-]+', value):
            raise ValueError('Unsafe runtime setting')
        matches = [i for i, line in enumerate(lines) if line.startswith(key + '=')]
        if len(matches) > 1:
            raise ValueError('Duplicate environment setting')
        if matches: lines[matches[0]] = key + '=' + value
        else: lines.append(key + '=' + value)
    return '\n'.join(lines) + '\n'


def compose():
    command('docker', 'compose', '--project-directory', str(ROOT), 'up', '-d', '--no-deps',
            '--no-build', '--pull', 'never', 'app', 'frontend', timeout=300)


def frontend_ok():
    opener = request.build_opener(request.ProxyHandler({}))
    with opener.open('http://127.0.0.1:18083/health', timeout=10) as response:
        if response.status != 200: raise RuntimeError('Backend unhealthy')
    with opener.open('http://192.168.85.199:8080/login', timeout=10) as response:
        body = response.read().decode()
    if '<title>SOLEX' not in body or '/assets/' not in body:
        raise RuntimeError('Frontend assets missing')


def wait_frontend(seconds=90):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            frontend_ok()
            return
        except (OSError, RuntimeError):
            time.sleep(2)
    raise RuntimeError('Frontend health check timed out')


def verify_migrations(expected_version):
    state = json.loads(sql("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;"))
    if state != {'version': expected_version, 'dirty': False}:
        raise RuntimeError('Database schema version or migration state is unexpected')
    admin = Client()
    admin.login(json.loads((ROOT / 'admin.json').read_text()))
    info = admin.call('GET', '/system/info')['data']
    if info.get('db_migration_failed') is not False or info.get('db_migration_error'):
        raise RuntimeError('Application reported a database migration failure')


def prepare_rollback_image(state_dir, source_image, release_id):
    """Keep the previous application binary with the additive migration source.

    golang-migrate requires the current DB version's SQL file even for a no-op.
    Retaining that source avoids a false migration failure when reverting code,
    and avoids dropping catalog rows or restoring over newly written user data.
    """
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9._/@:+-]+', source_image):
        raise ValueError('Invalid rollback image reference')
    context = state_dir / 'rollback-app'
    context.mkdir()
    shutil.copytree(SOURCE / 'app/migrations', context / 'migrations')
    (context / 'Dockerfile').write_text('FROM ' + source_image + '\nCOPY migrations/ /app/migrations/\n')
    (context / '.dockerignore').write_text('*\n!Dockerfile\n!migrations/\n!migrations/**\n')
    tag = 'solex/solex-app:rollback-' + release_id.removeprefix('solex-runtime-')
    command('docker', 'build', '--pull=false', '--network=none', '-t', tag, str(context), timeout=300)
    return tag


def deploy():
    meta = json.loads((SOURCE / 'release.json').read_text())
    release_id = meta['release_id']
    if not re.fullmatch('solex-runtime-[a-f0-9]{12}', release_id): raise ValueError('Invalid release ID')
    if ROOT.is_symlink() or not (ROOT / 'active.json').exists(): raise RuntimeError('Native deployment not found')
    state_dir = ROOT / 'runtime-releases' / release_id
    state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    if (state_dir / 'active.json').exists(): raise RuntimeError('Release is already active')
    # Both images are built before any live configuration change.
    for kind in ('app', 'ui'):
        command('docker', 'build', '--pull=false', '--network=none',
                '--build-arg', 'SOURCE_REVISION=' + meta['source_revision'],
                '--build-arg', 'RELEASE_ID=' + release_id,
                '-t', meta[kind + '_image'], str(SOURCE / kind), timeout=600)
    env_path = ROOT / '.env'
    old_env = env_path.read_text()
    settings = read_env(env_path)
    if settings.get('COMPOSE_FILE', 'compose.yaml') not in ('compose.yaml', 'compose.yaml:compose.file-generation.yaml'):
        raise RuntimeError('Review existing Compose overrides before enabling file generation')
    if command('docker', 'image', 'inspect', '--format', '{{.Id}}', file_generation_setup.IMAGE) != file_generation_setup.IMAGE_ID:
        raise RuntimeError('Build and verify the pinned file sandbox image before deploying')
    overlay = ROOT / 'compose.file-generation.yaml'
    old_overlay = overlay.read_text() if overlay.exists() else None
    if old_overlay is not None:
        private_write(state_dir / 'before-compose.file-generation.yaml', old_overlay)
    for service, key in [('app','WEKNORA_APP_IMAGE'),('frontend','WEKNORA_UI_IMAGE')]:
        running = command('docker','inspect','--format','{{.Image}}','weknora-native-' + service + '-1')
        configured = command('docker','image','inspect','--format','{{.Id}}',settings[key])
        if running != configured: raise RuntimeError('Saved image setting differs from the running service')
    schema_before = json.loads(sql("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;"))
    if (meta.get('postgres_schema_before'), meta.get('postgres_schema_after')) != (135, 136):
        raise RuntimeError('This release requires an explicitly reviewed additive schema transition')
    if schema_before.get('dirty') or schema_before.get('version') not in (135, 136):
        raise RuntimeError('Unexpected pre-release database schema; migration review required')
    rollback_image = prepare_rollback_image(state_dir, settings['WEKNORA_APP_IMAGE'], release_id)
    rollback_env = replace_env(old_env, {'WEKNORA_APP_IMAGE': rollback_image})
    if (state_dir / 'before.env').exists(): raise RuntimeError('Prior attempt exists; inspect before retry')
    private_write(state_dir / 'before.env', old_env)
    private_write(state_dir / 'rollback.env', rollback_env)
    backup = command('docker', 'exec', 'weknora-native-postgres-1', 'pg_dump', '-U', 'weknora', '-d', 'weknora', '-Fc', binary=True)
    backup_path = state_dir / 'before.pgdump'
    fd = os.open(backup_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream: stream.write(backup)
    bindings = sql("SELECT coalesce(json_agg(json_build_object('id',id,'embedding_model_id',embedding_model_id)), '[]') FROM knowledge_bases WHERE deleted_at IS NULL;")
    old_models = json.loads(sql("SELECT coalesce(json_agg(json_build_object('id',id,'is_builtin',is_builtin)), '[]') FROM models WHERE tenant_id=10000 AND deleted_at IS NULL;"))
    old_mcp = json.loads(sql("SELECT coalesce(json_agg(json_build_object('id',id,'is_builtin',is_builtin)), '[]') FROM mcp_services WHERE tenant_id=10000 AND deleted_at IS NULL;"))
    if len(old_models) < 2: raise RuntimeError('Expected existing configured models')
    save_json(state_dir / 'before-models.json', old_models)
    save_json(state_dir / 'before-mcp.json', old_mcp)
    try:
        # Keep original IDs, encrypted credentials, dimensions and model compatibility flags.
        sql("BEGIN; UPDATE models SET is_builtin=true WHERE tenant_id=10000 AND deleted_at IS NULL; UPDATE mcp_services SET is_builtin=true WHERE tenant_id=10000 AND deleted_at IS NULL; COMMIT;")
        new_env = replace_env(old_env, {'WEKNORA_APP_IMAGE':meta['app_image'], 'WEKNORA_UI_IMAGE':meta['ui_image'],
            'SOLEX_MANAGED_RUNTIME':'true', 'SOLEX_RUNTIME_TENANT_ID':'10000',
            'COMPOSE_FILE':'compose.yaml:compose.file-generation.yaml'})
        private_write(overlay, (SOURCE / 'compose.file-generation.yaml').read_text())
        private_write(env_path, new_env)
        compose()
        wait_health()
        wait_frontend()
        verify_migrations(meta['postgres_schema_after'])
        for service, key in [('app','app_image'),('frontend','ui_image')]:
            running = command('docker','inspect','--format','{{.Image}}','weknora-native-' + service + '-1')
            expected = command('docker','image','inspect','--format','{{.Id}}',meta[key])
            if running != expected: raise RuntimeError('Running service image does not match this release')
        file_generation_setup.configure(state_dir)
        checks = accept(state_dir)
        after_bindings = json.loads(sql("SELECT coalesce(json_agg(json_build_object('id',id,'embedding_model_id',embedding_model_id)), '[]') FROM knowledge_bases WHERE deleted_at IS NULL;"))
        old_by_id = {item['id']:item['embedding_model_id'] for item in json.loads(bindings)}
        new_by_id = {item['id']:item['embedding_model_id'] for item in after_bindings}
        if any(new_by_id.get(key) != value for key, value in old_by_id.items()):
            raise RuntimeError('Existing embedding binding changed')
        save_json(state_dir / 'active.json', {**meta, 'activated_at':time.time(),
            'existing_embedding_bindings_preserved':True, 'checks':checks})
        print('Managed runtime release is active. Ordinary-user inheritance and chat checks passed.')
    except Exception as exc:
        rollback = {'attempted_at':time.time(), 'failure_type':type(exc).__name__,
            'reason':str(exc) if isinstance(exc, RuntimeError) else 'Details omitted',
            'environment_restored':False, 'model_flags_restored':False, 'services_restored':False,
            'file_generation_restored':False}
        try:
            file_generation_setup.rollback(state_dir)
            rollback['file_generation_restored'] = True
        except Exception:
            pass
        try:
            private_write(env_path, rollback_env)
            if old_overlay is None:
                overlay.unlink(missing_ok=True)
            else:
                private_write(overlay, old_overlay)
            rollback['environment_restored'] = True
        except Exception:
            pass
        try:
            restore = []
            for table, rows in [('models', old_models), ('mcp_services', old_mcp)]:
                for row in rows:
                    if not re.fullmatch(r'[a-zA-Z0-9_-]+', row['id']): raise RuntimeError('Invalid rollback identifier')
                    restore.append(f"UPDATE {table} SET is_builtin={'true' if row['is_builtin'] else 'false'} WHERE id='{row['id']}' AND tenant_id=10000;")
            sql('BEGIN;' + ''.join(restore) + 'COMMIT;')
            rollback['model_flags_restored'] = True
        except Exception:
            pass
        # Do not let a database failure prevent restoration of the prior app.
        if rollback['environment_restored']:
            try:
                compose()
                wait_health()
                wait_frontend()
                verify_migrations(meta['postgres_schema_after'])
                rollback['services_restored'] = True
            except Exception:
                pass
        save_json(state_dir / 'rollback.json', rollback)
        restored = all(rollback[key] for key in ('environment_restored','model_flags_restored','services_restored','file_generation_restored'))
        status = 'previous app/UI/configuration restored' if restored else 'rollback incomplete; inspect protected rollback.json'
        raise RuntimeError('Release failed; ' + status + ': ' + (str(exc) if isinstance(exc, RuntimeError) else type(exc).__name__)) from None



if __name__ == '__main__':
    try: deploy()
    except Exception as exc:
        raise SystemExit(str(exc) if isinstance(exc, RuntimeError) else 'Release failed; inspect protected state') from None
