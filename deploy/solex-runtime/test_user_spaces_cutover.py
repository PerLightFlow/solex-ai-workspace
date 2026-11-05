"""Exercise the destructive rollback boundary with fake Docker and a temp FS."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('user_spaces_cutover',Path(__file__).with_name('deploy-user-spaces.py'))
cutover=importlib.util.module_from_spec(spec);spec.loader.exec_module(cutover)


class CutoverTests(unittest.TestCase):
    def simulate(self, fail_public=False):
        root=Path(self.enterContext(tempfile.TemporaryDirectory()))
        source=root/'release';source.mkdir()
        approved=root/'approved';(approved/'build-cache').mkdir(parents=True)
        revision='a'*40
        (source/'release.json').write_text(json.dumps({'source_revision':revision,'release_id':'solex-runtime-'+revision[:12],'authorization_model':'account_spaces_v1','postgres_schema_before':136,'postgres_schema_after':137,'app_image':'fixture/app:new','ui_image':'fixture/ui:new'}))
        (source/'rehearsal-acceptance.json').write_text(json.dumps({'passed':True,'source_revision':revision,'file_generation':{'passed':True}}))
        migrations=source/'app/migrations/versioned';migrations.mkdir(parents=True)
        (migrations/'000137_user_spaces.up.sql').write_text('SELECT 1;')
        original={'Config':{'Image':'fixture:old'},'HostConfig':{'NetworkMode':'fixture'}}
        containers={cutover.APP,cutover.FRONTEND};events=[]
        def renamed(a,b):containers.remove(a);containers.add(b);events.append(('rename',a,b))
        def cloned(old,name,*args,**kw):containers.add(name);events.append(('clone',name))
        def command(args,**kw):events.append(tuple(args));return b''
        def helper(code,state,**kw):
            for name in ['before-env','admin.json']:
                (state/name).write_text('{}')
        def db(query):
            if 'json_build_object' in query:return '{"version":136,"dirty":false}'
            return ''
        checks=[]
        def ui(url):
            checks.append(url)
            if fail_public and len(checks)==2:raise RuntimeError('public frontend probe failed')
        def smoke(state):
            if not fail_public:raise RuntimeError('candidate ordinary-user check failed')
            return {'passed':True}
        overrides={'SOURCE':source,'ALLOWED':approved,'inspect':lambda name:original,'exists':lambda name:name in containers,'action':lambda *a:events.append(('action',*a)),'clone':cloned,'rename':renamed,'remove':lambda name:containers.discard(name),'docker':lambda *a,**kw:None,'wait_health':lambda *a:None,'root_helper':helper,'sql':db,'migration':lambda *a:{'fingerprint':'synthetic'},'command':command,'smoke':smoke,'check_ui':ui}
        with patch.multiple(cutover,**overrides),patch.object(cutover.subprocess,'run',return_value=subprocess.CompletedProcess([],0,stdout=b'',stderr=b'')):
            with self.assertRaises(RuntimeError):cutover.deploy()
        return approved/'build-cache'/('user-spaces-cutover-'+revision[:12]),events,containers

    def test_failure_before_public_restores_database_and_service(self):
        state,events,containers=self.simulate()
        self.assertTrue((state/'rolled-back.json').exists())
        self.assertFalse((state/'public-open.json').exists())
        self.assertTrue(any('dropdb' in event for event in events))
        self.assertIn(cutover.APP,containers);self.assertIn(cutover.FRONTEND,containers)

    def test_failure_after_public_never_restores_old_database(self):
        state,events,_=self.simulate(True)
        self.assertTrue((state/'public-open.json').exists())
        self.assertTrue((state/'needs-forward-repair.json').exists())
        self.assertFalse(any('dropdb' in event for event in events))
        self.assertFalse((state/'rolled-back.json').exists())


if __name__=='__main__':unittest.main()
