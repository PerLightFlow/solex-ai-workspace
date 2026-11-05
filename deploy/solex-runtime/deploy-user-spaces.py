#!/usr/bin/env python3
"""Quiesced permission migration and Docker cutover, without sudo or Compose CLI.

A database rollback is allowed only before reopening the public frontend.
Once public-open.json exists, recovery must retain the new authorization model.
"""
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib import request, error
from api_client import Client, save_json
from docker_control import APP, FRONTEND, POSTGRES, ALLOWED, ROOT, action, clone, docker, exists, inspect, remove, rename, root_helper, sql, wait_health

SOURCE = Path(__file__).resolve().parent


def command(args, **kwargs):
    r = subprocess.run(args, capture_output=True, **kwargs)
    if r.returncode:
        raise RuntimeError('Deployment command failed; output kept private: ' + args[0])
    return r.stdout


def migration(apply=False, fingerprint=None):
    args=['python3',str(SOURCE/'migrate-user-spaces.py')]
    if apply:args += ['--apply','--expected-fingerprint',fingerprint]
    return json.loads(command(args, text=True))


def check_ui(url):
    opener=request.build_opener(request.ProxyHandler({}))
    deadline=time.monotonic()+60
    while time.monotonic()<deadline:
        try:
            with opener.open(url+'/login',timeout=10) as r:body=r.read().decode()
            if '<title>SOLEX' not in body or '/assets/' not in body:
                raise RuntimeError('Candidate frontend returned unexpected content')
            return
        except (OSError,error.URLError):
            # Docker's running state precedes nginx's first listener; its
            # entrypoint can accept and reset a connection during startup.
            time.sleep(1)
    raise RuntimeError('Candidate frontend readiness timed out')


def smoke(state):
    admin=Client();admin.login(json.loads((state/'admin.json').read_text()))
    caps=admin.call('GET','/system/capabilities')['data']
    if not caps.get('user_spaces') or not caps.get('managed_runtime'):raise RuntimeError('Permission mode is inactive')
    info=admin.call('GET','/system/info')['data']
    if info.get('db_migration_failed') or info.get('db_migration_error'):raise RuntimeError('Schema migration failed')
    admin.call('GET','/spaces');admin.call('GET','/spaces/retention')
    # New account inherits runtime defaults and has no access to admin documents.
    import secrets
    identity={'username':'__spaces_live_'+secrets.token_hex(3),'email':'spaces-live-'+secrets.token_hex(5)+'@example.invalid','password':secrets.token_urlsafe(16)+'Aa1!'}
    ordinary=Client();user=None
    try:
        user=ordinary.call('POST','/auth/register',identity)['user'];ordinary.login(identity)
        if ordinary.call('GET','/spaces')['data']:raise RuntimeError('New user inherited shared memberships')
        if ordinary.call('GET','/knowledge-bases')['data']:raise RuntimeError('New user inherited private knowledge')
        defaults=ordinary.call('GET','/tenants/kv/default-settings')['data']
        if not defaults.get('chat_model_id') or not defaults.get('embedding_model_id'):raise RuntimeError('Central model inheritance failed')
        from spaces_check import expect
        expect(ordinary,(403,),'POST','/models',{})
        expect(ordinary,(403,),'POST',f'/tenants/{user["tenant_id"]}/invitations',{'email':'no@example.invalid'})
        for item in admin.call('GET','/spaces')['data']:
            expect(ordinary,(403,),'GET','/spaces/'+item['id']+'/members')
        return {'passed':True,'registration_personal_only':True,'central_runtime_inherited':True,'unrelated_shared_spaces_denied':True}
    finally:
        if user:admin.call('POST','/system/admin/users/status',{'user_id':user['id'],'is_active':False})


def deploy():
    meta=json.loads((SOURCE/'release.json').read_text())
    if meta.get('authorization_model')!='account_spaces_v1' or (meta['postgres_schema_before'],meta['postgres_schema_after'])!=(136,137):
        raise RuntimeError('Unexpected authorization/schema transition')
    revision=meta['source_revision']
    if not re.fullmatch('[0-9a-f]{40}',revision):raise RuntimeError('Invalid revision')
    proof=json.loads((SOURCE/'rehearsal-acceptance.json').read_text())
    if not proof.get('passed') or not proof.get('file_generation',{}).get('passed') or proof.get('source_revision')!=revision or any(proof.get(k)!=meta.get(k) for k in ('backend_sha256','dist_sha256')):
        raise RuntimeError('This exact source must pass isolated collaboration and file-generation acceptance')
    command(['sha256sum','--check','--status','SHA256SUMS'],cwd=SOURCE)
    before=json.loads(sql("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations"))
    if before!={'version':136,'dirty':False}:raise RuntimeError('Expected clean schema 136; inspect prior deployment state')
    old_app,old_ui=inspect(APP),inspect(FRONTEND)
    for service,key in [('app','app_image'),('ui','ui_image')]:
        command(['docker','image','inspect',meta[key]])
    suffix=revision[:12]
    state=ALLOWED/'build-cache'/('user-spaces-cutover-'+suffix)
    state.mkdir(mode=0o700)  # refuses overwriting a previous attempt
    save_json(state/'before-app.json',old_app);save_json(state/'before-frontend.json',old_ui)
    root_helper("""import shutil,os
for name in ('.env','admin.json','active.json'):
 shutil.copyfile('/deployment/'+name,'/state/before-'+name.lstrip('.'))
shutil.copyfile('/deployment/admin.json','/state/admin.json')
st=os.stat('/state')
for name in os.listdir('/state'):
 p='/state/'+name
 if os.path.isfile(p):os.chmod(p,0o600);os.chown(p,st.st_uid,st.st_gid)
""",state)
    backup_app=APP+'-before-user-spaces-'+suffix
    backup_ui=FRONTEND+'-before-user-spaces-'+suffix
    if exists(backup_app) or exists(backup_ui):raise RuntimeError('Backup containers already exist')
    opened=False;stopped=False;database_backed_up=False;renamed_app=False;renamed_ui=False
    try:
        stopped=True;action(FRONTEND,'stop');action(APP,'stop')
        fd=os.open(state/'before.pgdump',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb') as out:
            p=subprocess.run(['docker','exec',POSTGRES,'pg_dump','-U','weknora','-d','weknora','-Fc'],stdout=out,stderr=subprocess.PIPE)
        if p.returncode:raise RuntimeError('Database backup failed before migration')
        database_backed_up=True
        root_helper("""import shutil
shutil.copytree('/deployment/data/files','/state/before-files')
""",state)
        preview=migration();save_json(state/'migration-preview.json',preview)
        sql('BEGIN;'+(SOURCE/'app/migrations/versioned/000137_user_spaces.up.sql').read_text()+'COMMIT;')
        applied=migration(True,preview['fingerprint']);save_json(state/'migration-applied.json',applied)
        sql('UPDATE schema_migrations SET version=137,dirty=FALSE;')
        # Disconnect old aliases so internal DNS cannot round-robin to a backup.
        rename(APP,backup_app);renamed_app=True
        network=old_app['HostConfig']['NetworkMode']
        docker('POST','/networks/'+network+'/disconnect',{'Container':backup_app,'Force':True})
        clone(old_app,APP,meta['app_image'],{'SOLEX_USER_SPACES':'true','WEKNORA_APP_IMAGE':meta['app_image'],'WEKNORA_UI_IMAGE':meta['ui_image']})
        action(APP,'start');wait_health(APP)
        checks=smoke(state);save_json(state/'smoke.json',checks)
        rename(FRONTEND,backup_ui);renamed_ui=True
        docker('POST','/networks/'+network+'/disconnect',{'Container':backup_ui,'Force':True})
        clone(old_ui,FRONTEND,meta['ui_image'])
        # Validate frontend through a loopback-only candidate first.
        candidate=FRONTEND+'-preflight-'+suffix
        clone(old_ui,candidate,meta['ui_image'],ports={'80/tcp':[{'HostIp':'127.0.0.1','HostPort':'18086'}]},isolated=True)
        try:
            action(candidate,'start');wait_health(candidate);check_ui('http://127.0.0.1:18086')
        finally:remove(candidate)
        updates={'WEKNORA_APP_IMAGE':meta['app_image'],'WEKNORA_UI_IMAGE':meta['ui_image'],'SOLEX_USER_SPACES':'true'}
        code="""import json,pathlib,os
changes=CHANGES
p=pathlib.Path('/deployment/.env');lines=p.read_text().splitlines()
for key,value in changes.items():
 found=[i for i,line in enumerate(lines) if line.startswith(key+'=')]
 if len(found)>1:raise RuntimeError('Duplicate environment key')
 if found:lines[found[0]]=key+'='+value
 else:lines.append(key+'='+value)
q=p.with_name('.env.user-spaces-pending');q.write_text('\\n'.join(lines)+'\\n');os.chmod(q,0o600);q.replace(p)
""".replace('CHANGES',repr(updates))
        root_helper(code,state,readonly=False)
        # Mark this point BEFORE public start: after it, never restore an old DB.
        save_json(state/'public-open.json',{'opened_at':time.time(),'source_revision':revision,'rollback_policy':'compatible-code-only'})
        opened=True
        action(FRONTEND,'start');wait_health(FRONTEND)
        check_ui('http://192.168.85.199:8080')
        save_json(state/'active.json',{**meta,'activated_at':time.time(),'checks':checks,'rehearsal':proof,'old_containers':[backup_app,backup_ui]})
        root_helper("""import shutil,pathlib,json
state=json.loads(pathlib.Path('/state/active.json').read_text())
p=pathlib.Path('/deployment/runtime-releases')/state['release_id'];p.mkdir(parents=True,exist_ok=False)
for name in ('active.json','migration-preview.json','migration-applied.json','smoke.json','public-open.json'):
 shutil.copyfile('/state/'+name,p/name)
pathlib.Path('/deployment/user-spaces-active.json').write_text(json.dumps(state,ensure_ascii=False,indent=2))
""",state,readonly=False)
        print('User spaces release is active; existing permissions preserved. Backup: '+str(state))
    except Exception as exc:
        save_json(state/'failure.json',{'type':type(exc).__name__,'message':str(exc)})
        if opened:
            save_json(state/'needs-forward-repair.json',{'time':time.time(),'public_writes_may_exist':True})
            raise RuntimeError('Public entry has opened: preserve database and repair forward; see protected state') from None
        if stopped:
            if renamed_app and exists(APP):remove(APP)
            if renamed_ui and exists(FRONTEND):remove(FRONTEND)
            if database_backed_up:
                command(['docker','exec',POSTGRES,'dropdb','-U','weknora','--force','weknora'])
                command(['docker','exec',POSTGRES,'createdb','-U','weknora','-T','template0','weknora'])
                with (state/'before.pgdump').open('rb') as inp:
                    p=subprocess.run(['docker','exec','-i',POSTGRES,'pg_restore','-U','weknora','-d','weknora','--exit-on-error'],stdin=inp,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
                if p.returncode:raise RuntimeError('Rollback restore failed; services kept stopped; inspect backup') from None
            root_helper("""import shutil,pathlib
shutil.copyfile('/state/before-env','/deployment/.env')
p=pathlib.Path('/state/before-files')
if p.exists():shutil.copytree(p,'/deployment/data/files',dirs_exist_ok=True)
""",state,readonly=False,image=old_app['Config']['Image'])
            for name,backup,old,renamed in [(APP,backup_app,old_app,renamed_app),(FRONTEND,backup_ui,old_ui,renamed_ui)]:
                if renamed:
                    if exists(backup):remove(backup)
                    clone(old,name)
                action(name,'start');wait_health(name)
            save_json(state/'rolled-back.json',{'time':time.time(),'before_public_open':True})
        raise RuntimeError('Cutover failed before public opening; previous service/database restored; inspect protected state') from None


if __name__=='__main__':
    deploy()
