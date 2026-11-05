"""Account-level collaboration acceptance using only synthetic fixture content."""
import argparse
import json
import secrets
import time
from pathlib import Path
from api_client import Client, ApiError, save_json
from file_generation_check import check_file_generation, binary_download


def expect(client, status, method, path, data=None, **kwargs):
    try:
        client.call(method, path, data, **kwargs)
    except ApiError as exc:
        if exc.status in status:
            return
        raise
    raise RuntimeError('Expected denial: ' + method + ' ' + path)


def run(base, state_dir, files=False, keep=False):
    admin = Client(base)
    admin.login(json.loads((state_dir / 'admin.json').read_text()))
    caps = admin.call('GET', '/system/capabilities')['data']
    assert caps.get('user_spaces') and caps.get('managed_runtime')
    identities, clients, users = [], [], []
    result = {'passed': False, 'checks': [], 'capacity_tested': False}
    def checked(name):
        result['checks'].append(name)
        save_json(state_dir / 'spaces-acceptance.json', result)
        print('PASS ' + name, flush=True)
    def rows(client, path):
        return client.call('GET', path)['data']
    def grant(client, space, resource, edit=False, download=False):
        return client.call('POST', f'/spaces/{space}/resources', {**resource, 'allow_edit':edit, 'allow_download':download})
    try:
        for i in range(3):
            identity = {'username':'__spaces_' + secrets.token_hex(4), 'email':'spaces-' + secrets.token_hex(6) + '@example.invalid', 'password':secrets.token_urlsafe(16) + 'Aa1!'}
            client = Client(base)
            user = client.call('POST', '/auth/register', identity)['user']
            client.login(identity)
            identities.append(identity);clients.append(client);users.append(user)
        a,b,c = clients
        save_json(state_dir / 'spaces-identities.json', {'identities': identities, 'users': users})
        ta,tb,tc = [u['tenant_id'] for u in users]
        assert len({ta,tb,tc}) == 3
        expect(a, (403,), 'POST', '/auth/switch-tenant', {'tenant_id':tb})
        expect(a, (403,), 'POST', f'/tenants/{ta}/invitations', {'email':identities[1]['email']})
        expect(a, (403,), 'POST', '/models', {})
        checked('fixed_personal_space_and_managed_configuration')
        space = a.call('POST', '/spaces', {'name':'验收协作组','description':'合成资料验收'})['data']['id']
        invitation = a.call('POST', f'/spaces/{space}/invitations', {'email': identities[1]['email']})['data']['id']
        expect(c, (404,), 'POST', f'/spaces/invitations/{invitation}/accept')
        b.call('POST', f'/spaces/invitations/{invitation}/accept')
        member = next(m for m in rows(b, f'/spaces/{space}/members') if m['user_id']==users[1]['id'])
        assert member['role']=='viewer'
        expect(b,(403,),'POST',f'/spaces/{space}/invitations',{'email':identities[2]['email']})
        expect(b,(403,),'PUT',f'/spaces/{space}/members/{users[1]["id"]}',{'role':'admin'})
        checked('email_bound_invitation_default_viewer')
        kb = a.call('POST','/knowledge-bases',{'name':'验收个人资料','type':'document'})['data']['id']
        resource = {'kind':'knowledge_base','id':kb,'source_tenant_id':ta}
        doc = a.call('POST',f'/knowledge-bases/{kb}/knowledge/manual',{'title':'设计原件','content':'合成项目采用 ESP32-P4；测试甲负责采集模块。','status':'draft'})['data']['id']
        expect(b,(403,404),'GET','/knowledge/'+doc)
        grant(a,space,resource)
        original=rows(b,'/knowledge/'+doc)
        assert original['content_revision']
        expect(b,(403,),'PUT','/knowledge/manual/'+doc,{'title':'越权修改','content':'不得写入','status':'draft','expected_revision':original['content_revision']})
        expect(b,(403,),'POST',f'/spaces/{space}/resources',{**resource,'allow_edit':True})
        a.call('PUT',f'/spaces/{space}/members/{users[1]["id"]}',{'role':'editor'})
        expect(b,(403,),'PUT','/knowledge/manual/'+doc,{'title':'越权修改','content':'不得写入','status':'draft','expected_revision':original['content_revision']})
        grant(a,space,resource,True,False)
        revision=rows(b,'/knowledge/'+doc)['content_revision']
        update={'title':'设计原件第二版','content':'合成项目采用 ESP32-P4；测试乙负责采集模块。','status':'draft','expected_revision':revision}
        b.call('PUT','/knowledge/manual/'+doc,update)
        expect(a,(409,),'PUT','/knowledge/manual/'+doc,{**update,'title':'过期覆盖'})
        history=rows(b,f'/spaces/knowledge/{doc}/versions')
        assert len(history['versions'])>=1
        latest=rows(a,'/knowledge/'+doc)
        assert latest.get('last_modified_by')==users[1]['id']
        checked('explicit_original_edit_actor_and_conflict')
        old=history['versions'][-1]['id']
        a.call('POST',f'/spaces/knowledge/{doc}/restore',{'version':old,'expected_revision':history['revision']})
        checked('source_version_restore')
        # Recycle a draft avoids model calls and still exercises source recovery.
        recycled=a.call('POST',f'/knowledge-bases/{kb}/knowledge/manual',{'title':'回收测试','content':'合成可恢复正文','status':'draft'})['data']['id']
        b.call('DELETE','/knowledge/'+recycled)
        deadline=time.monotonic()+60
        while time.monotonic()<deadline:
            if any(r['resource_id']==recycled for r in rows(b,f'/spaces/knowledge-bases/{kb}/recycle-bin')):break
            time.sleep(0.5)
        else:raise RuntimeError('Asynchronous deletion did not reach the recycle bin')
        b.call('POST',f'/spaces/recycle/{recycled}/restore')
        rows(b,'/knowledge/'+recycled)
        checked('document_recycle_and_recovery')
        a.call('PUT',f'/spaces/{space}/archive',{'archived':True})
        expect(b,(403,),'POST',f'/knowledge-bases/{kb}/knowledge/manual',{'title':'归档拒绝','content':'deny','status':'draft'})
        rows(b,'/knowledge/'+doc)
        a.call('PUT',f'/spaces/{space}/archive',{'archived':False})
        checked('archive_reads_without_collaborative_writes')
        # An unrelated personal resource is never exposed by sharing an agent.
        private=c.call('POST','/knowledge-bases',{'name':'第三人的私有资料','type':'document'})['data']['id']
        agent=b.call('POST','/agents',{'name':'共享验收助手','config':{'agent_mode':'smart-reasoning','kb_selection_mode':'all','skills_selection_mode':'none','mcp_selection_mode':'none'}})['data']
        ar={'kind':'agent','id':agent['id'],'source_tenant_id':tb}
        expect(b,(400,),'POST',f'/spaces/{space}/resources',ar)
        config={**agent['config'],'kb_selection_mode':'selected','knowledge_bases':[private]}
        expect(b,(400,403),'PUT','/agents/'+agent['id'],{'name':agent['name'],'config':config})
        config={**agent['config'],'kb_selection_mode':'selected','knowledge_bases':[kb]}
        b.call('PUT','/agents/'+agent['id'],{'name':agent['name'],'config':config})
        grant(b,space,ar)
        listing=rows(a,'/knowledge-bases?agent_id='+agent['id']+'&agent_source_tenant_id='+str(tb))
        assert any(r['id']==kb for r in listing) and not any(r['id']==private for r in listing)
        checked('shared_agent_explicit_cross_personal_kb_scope')
        # A second path grants reading but cannot retain edit rights from the first.
        space2=a.call('POST','/spaces',{'name':'验收只读组'})['data']['id']
        invite=a.call('POST',f'/spaces/{space2}/invitations',{'email':identities[1]['email']})['data']['id']
        b.call('POST',f'/spaces/invitations/{invite}/accept');grant(a,space2,resource)
        a.call('DELETE',f'/spaces/{space}/resources',resource)
        access=rows(b,f'/spaces/knowledge-bases/{kb}/permission')
        assert access['read'] and not access['edit'] and not access['download']
        a.call('DELETE',f'/spaces/{space2}/resources',resource)
        expect(b,(403,404),'GET','/knowledge/'+doc)
        checked('multiple_grant_union_and_revocation')
        # A space-owned KB remains with the team after its creator departs.
        owned=a.call('POST','/knowledge-bases',{'name':'团队自有资料','type':'document'},extra_headers={'X-Solex-Space-ID':space})['data']['id']
        assert not any(r['id']==owned for r in rows(a,'/knowledge-bases'))
        a.call('POST',f'/spaces/{space}/transfer',{'user_id':users[1]['id']})
        a.call('DELETE',f'/spaces/{space}/members/{users[0]["id"]}')
        expect(a,(403,404),'GET','/knowledge-bases/'+owned)
        rows(b,'/knowledge-bases/'+owned)
        expect(b,(400,409),'DELETE','/spaces/'+space)
        checked('space_owned_resource_survives_creator_departure')
        b.call('DELETE','/knowledge-bases/'+owned)
        assert any(r['resource_id']==owned for r in rows(b,'/spaces/recycled-resources'))
        b.call('POST','/spaces/recycled-resources/restore',{'kind':'knowledge_base','id':owned,'tenant_id':ta})
        rows(b,'/knowledge-bases/'+owned)
        checked('whole_resource_recycle_and_restore')
        expect(b,(403,),'PUT','/spaces/retention',{'manual_versions':30,'automatic_versions':30,'recycle_days':30})
        assert rows(admin,'/spaces/retention')['manual_versions']>0
        checked('administrator_retention_controls')
        if files:
            result['file_generation']=check_file_generation(admin,c)
            checked('real_python_xlsx_generation_and_private_download')
        result['passed']=True
        return result
    finally:
        if not keep:
            for user in users:
                admin.call('POST','/system/admin/users/status',{'user_id':user['id'],'is_active':False})
        save_json(state_dir / 'spaces-acceptance.json',result)


if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--base',default='http://127.0.0.1:18084');p.add_argument('--state',required=True);p.add_argument('--files',action='store_true');p.add_argument('--keep',action='store_true')
    args=p.parse_args();run(args.base,Path(args.state),args.files,args.keep)
