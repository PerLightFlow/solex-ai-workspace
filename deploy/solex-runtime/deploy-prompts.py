#!/usr/bin/env python3
"""Apply the Solex prompt-branding release to the existing native Docker app.

Run from a checked release containing release.json, config/, and api_client.py.
The backend binary, frontend, models, and user-authored prompts stay in place.
"""
import json
from pathlib import Path
import re
import secrets
import subprocess
import time

from api_client import ROOT, Client, private_write, read_env, save_json, wait_health

SOURCE = Path(__file__).resolve().parent
TEMPLATES = '/tenants/kv/prompt-templates'
CONFIG_FILES = {
    'builtin_agents.yaml', 'prompt_templates/agent_system_prompt.yaml',
    'prompt_templates/intent_prompts.yaml', 'prompt_templates/system_prompt.yaml',
    'prompt_templates/fallback.yaml',
}


def command(*args, input=None):
    result = subprocess.run(args, input=input, capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed; output omitted')
    return result.stdout.strip()


def sql(statement):
    return command('docker', 'exec', '-i', 'weknora-native-postgres-1', 'psql',
                   '-v', 'ON_ERROR_STOP=1', '-U', 'weknora', '-d', 'weknora', '-At', input=statement)


def quoted(value):
    return "'" + value.replace("'", "''") + "'"


def rebrand(text):
    return text.replace('WeKnora', 'Solex').replace(' developed by Tencent', '')


def sync_seeded_prompts(changes, reverse=False):
    # An exact prior-body match plus customized=false protects admin edits.
    # Never modify deleted rows or re-seed a removed built-in.
    before, after = ('after', 'before') if reverse else ('before', 'after')
    data = quoted(json.dumps(changes))
    return sql(f"""WITH changed AS (
        UPDATE system_builtin_agents AS a
        SET config=jsonb_set(a.config, '{{system_prompt}}', to_jsonb(p.{after})), updated_at=now()
        FROM jsonb_to_recordset({data}::jsonb) AS p(id text, before text, after text)
        WHERE a.id=p.id AND NOT a.customized AND a.deleted_at IS NULL
          AND a.config->>'system_prompt'=p.{before}
        RETURNING a.id
    ) SELECT count(*) FROM changed;""")


def restart():
    command('docker', 'compose', '--project-directory', str(ROOT), 'up', '-d',
            '--no-deps', '--no-build', '--pull', 'never', 'app')
    wait_health()


def check_ordinary(admin, expected, state_dir):
    client, user_id = Client(), None
    state = {'username': '__prompt_check_' + secrets.token_hex(4),
             'email': 'prompt-' + secrets.token_hex(6) + '@example.invalid',
             'password': secrets.token_urlsafe(18) + 'Aa1!'}
    try:
        user_id = client.call('POST', '/auth/register', state)['user']['id']
        if client.login(state)['user'].get('is_system_admin'):
            raise RuntimeError('Expected an ordinary test account')
        if client.call('GET', TEMPLATES)['data'] != expected:
            raise RuntimeError('Ordinary-user prompt templates differ')
    finally:
        if user_id:
            admin.call('POST', '/system/admin/users/status', {'user_id': user_id, 'is_active': False})
            save_json(state_dir / 'test-account.json', {'user_id': user_id, 'disabled': True})


def deploy():
    meta = json.loads((SOURCE / 'release.json').read_text())
    revision = meta['source_revision']
    if not re.fullmatch('[a-f0-9]{40}', revision):
        raise ValueError('Expected a full source commit')
    config = SOURCE / 'config'
    paths = list(config.rglob('*'))
    if config.is_symlink() or any(p.is_symlink() for p in paths):
        raise ValueError('Config symlinks are not allowed')
    if {p.relative_to(config).as_posix() for p in paths if p.is_file()} != CONFIG_FILES:
        raise ValueError('Expected exactly the five public branding configuration files')
    release_id = 'solex-prompts-' + revision[:12]
    state_dir = ROOT / 'prompt-releases' / release_id
    state_dir.mkdir(parents=True, mode=0o700, exist_ok=False)
    env_path = ROOT / '.env'
    old_env = env_path.read_text()
    old_image = read_env(env_path)['WEKNORA_APP_IMAGE']
    running = command('docker', 'inspect', '--format', '{{.Image}}', 'weknora-native-app-1')
    if running != command('docker', 'image', 'inspect', '--format', '{{.Id}}', old_image):
        raise RuntimeError('Saved app image differs from the running image')
    untouched = {s: command('docker', 'inspect', '--format', '{{.Id}}', 'weknora-native-' + s + '-1')
                 for s in ('frontend', 'postgres', 'redis', 'embedding', 'docreader', 'searxng')}
    admin = Client()
    admin.login(json.loads((ROOT / 'admin.json').read_text()))
    previous = admin.call('GET', TEMPLATES)['data']
    expected = json.loads(rebrand(json.dumps(previous)))
    if expected == previous:
        raise RuntimeError('No old prompt branding found; inspect before reapplying')
    prompt_map = {t['content']: rebrand(t['content'])
                  for values in previous.values() if isinstance(values, list)
                  for t in values if t.get('content') and rebrand(t['content']) != t['content']}
    records = json.loads(sql("SELECT coalesce(json_agg(json_build_object('id',id,'before',config->>'system_prompt')), '[]') FROM system_builtin_agents WHERE NOT customized AND deleted_at IS NULL;"))
    changes = [{**r, 'after': prompt_map[r['before']]} for r in records if r['before'] in prompt_map]
    save_json(state_dir / 'seeded-prompts.json', changes)
    save_json(state_dir / 'templates-before.json', previous)
    private_write(state_dir / 'before.env', old_env)
    image = 'solex/solex-app:prompts-' + revision[:12]
    # Pin the existing image ID so the compiled runtime is unchanged.
    (SOURCE / 'Dockerfile').write_text(
        'FROM ' + running + '\nLABEL io.solex.prompts.revision="' + revision + '"\n'
        'COPY config/ /app/config/\n')
    (SOURCE / '.dockerignore').write_text('*\n!Dockerfile\n!config/\n!config/**\n')
    command('docker', 'build', '--pull=false', '--network=none', '-t', image, str(SOURCE))
    lines = old_env.splitlines()
    if sum(line.startswith('WEKNORA_APP_IMAGE=') for line in lines) != 1:
        raise RuntimeError('Expected exactly one app image setting')
    new_env = '\n'.join('WEKNORA_APP_IMAGE=' + image if line.startswith('WEKNORA_APP_IMAGE=') else line for line in lines) + '\n'
    try:
        updated = int(sync_seeded_prompts(changes))
        private_write(env_path, new_env)
        restart()
        admin.login(json.loads((ROOT / 'admin.json').read_text()))
        if admin.call('GET', TEMPLATES)['data'] != expected:
            raise RuntimeError('Published templates do not match the branding-only transformation')
        check_ordinary(admin, expected, state_dir)
        for service, container_id in untouched.items():
            if command('docker', 'inspect', '--format', '{{.Id}}', 'weknora-native-' + service + '-1') != container_id:
                raise RuntimeError('An unrelated service was recreated')
        save_json(state_dir / 'active.json', {**meta, 'image': image, 'previous_image': old_image,
                  'activated_at': time.time(), 'seeded_prompts_updated': updated,
                  'admin_and_ordinary_templates_passed': True, 'other_containers_unchanged': True})
        print(f'Solex prompt release active; admin and ordinary-user templates verified; {updated} unmodified built-in prompts synchronized.')
    except Exception as exc:
        rollback = {'failure_type': type(exc).__name__, 'environment_restored': False,
                    'app_restored': False, 'seeded_prompts_restored': False}
        # Recovery steps are independent: a transient database failure must not
        # prevent an attempt to return the app to its prior image.
        try:
            private_write(env_path, old_env)
            rollback['environment_restored'] = True
        except Exception:
            pass
        if rollback['environment_restored']:
            try:
                restart()
                rollback['app_restored'] = True
            except Exception:
                pass
        try:
            sync_seeded_prompts(changes, reverse=True)
            rollback['seeded_prompts_restored'] = True
        except Exception:
            pass
        save_json(state_dir / 'rollback.json', rollback)
        raise


if __name__ == '__main__':
    try:
        deploy()
    except Exception as exc:
        raise SystemExit('Prompt deployment failed (' + type(exc).__name__ + '); inspect protected state, no credentials printed') from None
