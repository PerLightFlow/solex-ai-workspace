import importlib.util
from pathlib import Path
import sys
import json
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
from deploy import replace_env, wait_frontend, prepare_rollback_image, verify_migrations
from acceptance import acceptance_identity

class RuntimeReleaseTests(unittest.TestCase):
    def test_preserves_credentials_and_unrelated_environment_exactly(self):
        raw = "# deployment\nDB_PASSWORD=literal-$-not-a-shell-command\nWEKNORA_APP_IMAGE=old:tag\n"
        updated = replace_env(raw, {'WEKNORA_APP_IMAGE':'solex/app:abc', 'SOLEX_MANAGED_RUNTIME':'true'})
        self.assertIn('DB_PASSWORD=literal-$-not-a-shell-command\n', updated)
        self.assertIn('WEKNORA_APP_IMAGE=solex/app:abc\n', updated)
        self.assertEqual(updated.count('WEKNORA_APP_IMAGE='), 1)
        self.assertIn('SOLEX_MANAGED_RUNTIME=true\n', updated)
    def test_rejects_ambiguous_environment(self):
        with self.assertRaises(ValueError):
            replace_env('WEKNORA_APP_IMAGE=a\nWEKNORA_APP_IMAGE=b\n', {'WEKNORA_APP_IMAGE':'c'})
    def test_rejects_untrusted_image_reference(self):
        with self.assertRaises(ValueError):
            replace_env('', {'WEKNORA_APP_IMAGE':'a\nDB_PASSWORD=x'})
    def test_acceptance_identity_satisfies_complex_password_policy(self):
        identity = acceptance_identity()
        self.assertGreaterEqual(len(identity['password']), 8)
        self.assertLessEqual(len(identity['password']), 32)
        self.assertRegex(identity['password'], r'[A-Za-z]')
        self.assertRegex(identity['password'], r'[0-9]')
        self.assertLessEqual(len(identity['username']), 50)
        self.assertTrue(identity['email'].endswith('@example.invalid'))
    def test_frontend_startup_tolerates_transient_connection_reset(self):
        with patch('deploy.frontend_ok', side_effect=[ConnectionResetError(), None]) as probe, patch('deploy.time.sleep'):
            wait_frontend()
        self.assertEqual(probe.call_count, 2)
    def test_frontend_failure_remains_a_release_failure(self):
        with patch('deploy.frontend_ok', side_effect=RuntimeError('Frontend assets missing')), patch('deploy.time.sleep'), patch('deploy.time.monotonic', side_effect=[0, 1, 91]):
            with self.assertRaisesRegex(RuntimeError, 'Frontend health check timed out'):
                wait_frontend()

    def test_rollback_image_keeps_old_binary_and_includes_new_schema_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            scripts = root / 'source/app/migrations/versioned'
            scripts.mkdir(parents=True)
            migration = scripts / '000136_knowledge_provenance.up.sql'
            migration.write_text('CREATE TABLE IF NOT EXISTS example (id text);')
            state = root / 'state'
            state.mkdir()
            with patch('deploy.SOURCE', root / 'source'), patch('deploy.command') as command:
                image = prepare_rollback_image(state, 'solex/solex-app:previous', 'solex-runtime-123456789abc')
            dockerfile = (state / 'rollback-app/Dockerfile').read_text()
            self.assertIn('FROM solex/solex-app:previous\n', dockerfile)
            self.assertNotIn('COPY WeKnora', dockerfile)
            self.assertEqual((state / 'rollback-app/migrations/versioned' / migration.name).read_bytes(), migration.read_bytes())
            self.assertEqual(image, 'solex/solex-app:rollback-123456789abc')
            command.assert_called_once()

    def test_schema_validation_does_not_accept_health_alone(self):
        for state in ({'version': 135, 'dirty': False}, {'version': 136, 'dirty': True}):
            with patch('deploy.sql', return_value=json.dumps(state)):
                with self.assertRaisesRegex(RuntimeError, 'schema version'):
                    verify_migrations(136)
        with patch('deploy.sql', return_value='{"version":136,"dirty":false}'), patch('deploy.Client') as client, patch('deploy.Path.read_text', return_value='{}'):
            client.return_value.call.return_value = {'data': {'db_migration_failed': True}}
            with self.assertRaisesRegex(RuntimeError, 'migration failure'):
                verify_migrations(136)

if __name__ == '__main__': unittest.main()
