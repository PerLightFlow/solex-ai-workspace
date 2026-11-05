import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('migration', Path(__file__).with_name('migrate-user-spaces.py'))
migration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(migration)


class MigrationTests(unittest.TestCase):
    def setUp(self):
        self.data = {
            'users': [{'id': 'owner', 'tenant_id': 1}, {'id': 'reader', 'tenant_id': 2}],
            'members': [{'user_id': 'owner', 'tenant_id': 1, 'role': 'owner'}, {'user_id': 'reader', 'tenant_id': 2, 'role': 'owner'}],
            'organizations': [{'id': 'team', 'owner_id': 'owner', 'owner_tenant_id': 1}],
            'organization_members': [{'organization_id': 'team', 'tenant_id': 1, 'role': 'admin'}, {'organization_id': 'team', 'tenant_id': 2, 'role': 'viewer'}],
            'knowledge_bases': [{'id': 'kb', 'tenant_id': 1}], 'agents': [],
            'kb_shares': [{'id': 'share', 'knowledge_base_id': 'kb', 'organization_id': 'team', 'source_tenant_id': 1, 'permission': 'viewer', 'shared_by_user_id': 'owner'}],
            'agent_shares': [],
        }

    def test_preserves_permissions_and_private_home(self):
        p = migration.plan(self.data, 1)
        self.assertTrue(migration.verify_permissions(self.data, p)['read_edit_download_unchanged'])
        self.assertEqual(p['personal_accounts'], 2)
        self.assertFalse(p['grants'][0]['edit'])
        self.assertFalse(p['grants'][0]['download'])
        self.assertEqual([x['role'] for x in p['members'] if x['user'] == 'reader'], ['viewer'])

    def test_refuses_implicit_personal_membership_mapping(self):
        self.data['members'].append({'user_id': 'reader', 'tenant_id': 1, 'role': 'viewer'})
        with self.assertRaises(ValueError):
            migration.plan(self.data, 1)

    def test_permission_comparison_catches_escalation(self):
        p = migration.plan(self.data, 1)
        p['grants'][0]['download'] = True
        with self.assertRaises(ValueError):
            migration.verify_permissions(self.data, p)

    def test_refuses_automatic_agent_dependency_inheritance(self):
        self.data['agents'] = [{'id': 'agent', 'tenant_id': 1}]
        self.data['agent_shares'] = [{**self.data['kb_shares'][0], 'agent_id': 'agent'}]
        with self.assertRaises(ValueError):
            migration.plan(self.data, 1)

    def test_apply_is_atomic_and_never_deletes_legacy_data(self):
        sql = migration.apply_sql(self.data, migration.plan(self.data, 1), 'f' * 64)
        self.assertTrue(sql.startswith('BEGIN;'))
        self.assertTrue(sql.endswith('COMMIT;'))
        self.assertNotIn('DELETE FROM', sql)
        self.assertIn('Already migrated', sql)


if __name__ == '__main__':
    unittest.main()
