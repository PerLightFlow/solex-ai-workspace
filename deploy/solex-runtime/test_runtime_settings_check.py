import io
import json
from types import SimpleNamespace
from urllib.error import HTTPError
import unittest

from api_client import Client
from runtime_settings_check import check_runtime_settings


class RuntimeSettingsAcceptanceTests(unittest.TestCase):
    def setUp(self):
        self.values = {'agent.max_iterations': 50,
                       'agent.max_completion_tokens': 16384,
                       'agent.collaboration_wait_timeout_seconds': 30}
        self.allow_invalid = False
        self.allow_ordinary = False
        self.writes = []
        self.admin = self.client(True)
        self.ordinary = self.client(False)

    def client(self, admin):
        client = Client()

        def respond(req, timeout):
            method = req.get_method()
            key = req.full_url.rsplit('/', 1)[-1]
            if not admin and not self.allow_ordinary:
                raise HTTPError(req.full_url, 403, '', {}, None)
            if method == 'GET':
                payload = [{'key': key, 'value': value, 'requires_restart': False}
                           for key, value in self.values.items()]
            elif method == 'PUT':
                value = json.loads(req.data)['value']
                minimum, maximum = {'agent.max_iterations': (1, 200), 'agent.max_completion_tokens': (8192, 65536),
                                    'agent.collaboration_wait_timeout_seconds': (1, 60)}[key]
                if not self.allow_invalid and (type(value) is not int or not minimum <= value <= maximum):
                    raise HTTPError(req.full_url, 400, '', {}, None)
                self.values[key] = value
                self.writes.append((key, value))
                payload = {'key': key, 'value': value}
            else:
                payload = {'success': True}
            return io.BytesIO(json.dumps(payload).encode())

        client.opener = SimpleNamespace(open=respond)
        return client

    def test_raw_settings_list_and_permissions_keep_effective_values(self):
        result = check_runtime_settings(self.admin, self.ordinary)
        self.assertTrue(result['passed'])
        self.assertEqual(result['values'], self.values)
        self.assertEqual(self.writes, [('agent.max_iterations', 50),
                                       ('agent.max_completion_tokens', 16384),
                                       ('agent.collaboration_wait_timeout_seconds', 30)])

    def test_invalid_write_acceptance_fails_and_restores_value(self):
        self.allow_invalid = True
        with self.assertRaisesRegex(RuntimeError, 'Invalid runtime setting was accepted'):
            check_runtime_settings(self.admin, self.ordinary)
        self.assertEqual(self.values['agent.max_iterations'], 50)

    def test_ordinary_write_acceptance_fails_and_restores_value(self):
        self.allow_ordinary = True
        with self.assertRaisesRegex(RuntimeError, 'Ordinary user changed'):
            check_runtime_settings(self.admin, self.ordinary)
        self.assertEqual(self.values['agent.max_iterations'], 50)


if __name__ == '__main__':
    unittest.main()
