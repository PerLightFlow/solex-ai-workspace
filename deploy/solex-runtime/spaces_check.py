"""Account-level collaboration acceptance using only synthetic fixture content."""
import argparse
import json
import secrets
import time
from pathlib import Path
from api_client import Client, ApiError, save_json
from file_generation_check import check_file_generation, binary_download
from urllib import request


def expect(client, status, method, path, data=None, **kwargs):
    try:
        client.call(method, path, data, **kwargs)
    except ApiError as exc:
        if exc.status in status:
            return
        raise
    raise RuntimeError('Expected denial: ' + method + ' ' + path)


def check_shared_knowledge(a, b, c, space, resource, agent, users, identities):
    kb=resource['id']
    marker='SPACE-'+secrets.token_hex(8)
    content=('合成协作核验资料：校验码为 '+marker+'。采集芯片为 ESP32-P4。测试甲负责采集模块。').encode()
    knowledge=a.call('POST',f'/knowledge-bases/{kb}/knowledge/file',upload=('collaboration.txt',content))['data']['id']
    def grant(download):
        a.call('POST',f'/spaces/{space}/resources',{**resource,'allow_edit':True,'allow_download':download})
    def deny_download(client,path):
        try:binary_download(client,path)
        except ApiError as exc:
            if exc.status not in (403,404):raise
        else:raise RuntimeError('Original file was accessible without download permission')
    for suffix in ('download','preview'):
        deny_download(b,f'/knowledge/{knowledge}/{suffix}')
    a.call('PUT',f'/spaces/{space}/members/{users[1]["id"]}',{'role':'viewer'})
    grant(True)
    assert binary_download(b,f'/knowledge/{knowledge}/download')==content
    assert binary_download(b,f'/knowledge/{knowledge}/preview')==content
    grant(False)
    deny_download(b,f'/knowledge/{knowledge}/download')
    a.call('PUT',f'/spaces/{space}/members/{users[1]["id"]}',{'role':'editor'})
    deadline=time.monotonic()+180
    while time.monotonic()<deadline:
        status=a.call('GET','/knowledge/'+knowledge)['data']['parse_status']
        if status=='completed':break
        if status in ('failed','cancelled'):raise RuntimeError('Synthetic source parsing failed')
        time.sleep(1)
    else:raise RuntimeError('Synthetic source indexing timed out')
    recall=b.call('POST',f'/knowledge-bases/{kb}/hybrid-search',{'query_text':'合成协作核验资料 校验码 采集芯片','match_count':5})
    assert marker in json.dumps(recall,ensure_ascii=False)
    invitation=a.call('POST',f'/spaces/{space}/invitations',{'email':identities[2]['email']})['data']['id']
    c.call('POST',f'/spaces/invitations/{invitation}/accept')
    session=c.call('POST','/sessions',{'title':'__shared_knowledge_acceptance__','agent_id':agent['id']})['data']['id']
    payload={'query':'请检索明确授权的“合成协作核验资料”，回答资料中的校验码和采集芯片。不联网，不委派。','agent_id':agent['id'],'agent_source_tenant_id':users[1]['tenant_id'],'agent_enabled':True,'web_search_enabled':False,'disable_title':True}
    req=request.Request(c.base+'/api/v1/agent-chat/'+session,data=json.dumps(payload).encode(),method='POST',headers={'Authorization':'Bearer '+c.token,'Content-Type':'application/json','Accept':'text/event-stream'})
    answer='';complete=False
    try:
        with c.opener.open(req,timeout=180) as stream:
            for raw in stream:
                line=raw.decode().strip()
                if not line.startswith('data:') or line[5:].strip() in ('','[DONE]'):continue
                frame=json.loads(line[5:]);kind=frame.get('response_type')
                if kind=='error':raise RuntimeError('Shared agent stream failed')
                if kind=='answer':answer+=frame.get('content','')
                if kind=='complete':complete=True;break
        assert complete and marker in answer and 'ESP32' in answer
        expect(a,(403,404),'GET',f'/sessions/{session}/messages')
        a.call('DELETE',f'/spaces/{space}/members/{users[2]["id"]}')
        expect(c,(403,404),'POST',f'/knowledge-bases/{kb}/hybrid-search',{'query_text':'合成协作核验资料'})
        expect(c,(403,404),'GET','/knowledge/'+knowledge)
    finally:c.call('DELETE','/sessions/'+session)
    return {'source_download_grant':True,'source_preview_grant':True,'indexing_and_cross_personal_search':True,'real_shared_agent_answer':True,'private_chat':True,'member_revocation':True}


def check_shared_wiki(a,b,space,tenant):
    kb=a.call('POST','/knowledge-bases',{'name':'合成 Wiki 协作','type':'document','indexing_strategy':{'wiki_enabled':True},'wiki_config':{}})['data']['id']
    a.call('POST',f'/spaces/{space}/resources',{'kind':'knowledge_base','id':kb,'source_tenant_id':tenant,'allow_edit':True})
    path=f'/knowledgebase/{kb}/wiki'
    page=b.call('POST',path+'/pages',{'slug':'synthetic-test','title':'合成页面','page_type':'concept','content':'初始正文'})
    assert page['tenant_id']==tenant
    expect(b,(428,),'PUT',path+'/pages/synthetic-test',{'content':'无版本不能覆盖'})
    changed=b.call('PUT',path+'/pages/synthetic-test',{'content':'协作修订正文','version':page['version']})
    expect(a,(409,),'PUT',path+'/pages/synthetic-test',{'content':'过期写入','version':page['version']})
    history=b.call('GET',path+'/revisions/synthetic-test')
    assert len(history['revisions'])>=1 and history['revisions'][0]['editor_id']
    expect(a,(409,),'POST',path+'/revert',{'slug':'synthetic-test','version':page['version'],'expected_version':page['version']})
    restored=a.call('POST',path+'/revert',{'slug':'synthetic-test','version':page['version'],'expected_version':changed['version']})
    assert restored['content']=='初始正文' and restored['version']>changed['version']
    b.call('DELETE',path+'/pages/synthetic-test')
    b.call('POST',f'/spaces/recycle/{page["id"]}/restore?kind=wiki')
    assert b.call('GET',path+'/pages/synthetic-test')['content']=='初始正文'
    source=('合成项目：北极星验证仪。此文件仅用于系统验收。\n'
            '主控选用 ESP32-P4，采集模块由合成用户甲负责，显示模块由合成用户乙负责。\n'
            '项目采用离线图像检测，摄像头输出 RGB 数据，通过 MIPI CSI 接口传输。\n'
            '设计决策：为降低网络依赖，检测结果在本地显示，资料保存在企业知识库。\n'
            '硬件版本为 V1，软件版本为 V2。\n').encode()
    b.call('POST',f'/knowledge-bases/{kb}/knowledge/file',upload=('wiki-fixture.txt',source))
    deadline=time.monotonic()+300
    while time.monotonic()<deadline:
        pages=b.call('GET',path+'/pages')['pages']
        if any(p['slug']!='synthetic-test' for p in pages):break
        time.sleep(2)
    else:raise RuntimeError('Automatic Wiki generation did not publish a synthetic page')
    return {'shared_writer_storage_scope':True,'optimistic_version':True,'attribution':True,'restore':True,'recycle':True,'automatic_ingest':True}


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
        result['knowledge_collaboration']=check_shared_knowledge(a,b,c,space,resource,agent,users,identities)
        checked('shared_source_download_retrieval_and_real_agent')
        result['wiki_collaboration']=check_shared_wiki(a,b,space,ta)
        checked('shared_wiki_conflict_attribution_and_recovery')
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
