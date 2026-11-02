"""Artifact-boundary checks; fixtures are synthetic bytes, never executed."""
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location(
    'prepare_release', Path(__file__).with_name('prepare-release.py'))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
REVISION = '1234567890abcdef1234567890abcdef12345678'
OTHER_REVISION = 'abcdef1234567890abcdef1234567890abcdef12'


class PrepareReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.backend = self.root / 'backend'
        self.dist = self.root / 'dist'
        self.output = self.root / 'release'
        self.backend.mkdir()
        self.dist.mkdir()
        self.write_binary()
        (self.backend / 'go-build-info.txt').write_text(
            '/out/WeKnora: go1.26.0\n'
            '\tbuild\t-tags=anydoc\n'
            '\tbuild\t-ldflags="-X \'github.com/Tencent/WeKnora/internal/handler.CommitID='
            + REVISION + '\'"\n')
        (self.backend / 'runtime-libraries.txt').write_text(
            'libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6 (0x12345678)\n')
        (self.dist / 'index.html').write_text(
            '<!doctype html><title>SOLEX 企业知识工作台</title>'
            '<script type="module" src="/assets/main.js"></script>')
        (self.dist / 'assets').mkdir()
        (self.dist / 'assets/main.js').write_text('console.log("SOLEX");\n')

    def write_binary(self, revision=REVISION):
        binary = b'\x7fELF\x00synthetic-not-executable\x00build\t-tags=anydoc\nCommitID=' + revision.encode()
        (self.backend / 'WeKnora').write_bytes(binary)
        (self.backend / 'WeKnora.sha256').write_text(
            hashlib.sha256(binary).hexdigest() + '  /out/WeKnora\n')

    def prepare(self, revision=REVISION):
        return release.prepare(self.backend, self.dist, self.output, revision)

    def test_complete_package_uses_pinned_runtimes_and_checks_every_file(self):
        # Backend directories may contain local build state. Only named public
        # artifacts enter the package, never this extra configuration file.
        (self.backend / '.env').write_text('LOCAL_ONLY=not-for-distribution\n')
        metadata = self.prepare()
        self.assertEqual(metadata['release_id'], 'solex-runtime-' + REVISION[:12])
        self.assertEqual(metadata['app_image'], 'solex/solex-app:' + REVISION[:12])
        self.assertEqual(metadata['ui_image'], 'solex/solex-ui:managed-' + REVISION[:12])
        dockerfile = (self.output / 'app/Dockerfile').read_text()
        self.assertIn('FROM ' + release.APP_RUNTIME + '\n', dockerfile)
        self.assertIn('COPY WeKnora /app/WeKnora\n', dockerfile)
        self.assertIn('COPY migrations/ /app/migrations/\n', dockerfile)
        self.assertIn('COPY config/ /app/config/\n', dockerfile)
        expected_config = [release.CONFIG / 'builtin_agents.yaml',
                           *sorted((release.CONFIG / 'prompt_templates').glob('*.yaml'))]
        for path in expected_config:
            self.assertEqual(path.read_bytes(),
                             (self.output / 'app/config' / path.relative_to(release.CONFIG)).read_bytes())
        self.assertFalse((self.output / 'app/config/config.yaml').exists())
        for dialect in ('versioned', 'sqlite'):
            expected = {p.name: p.read_bytes() for p in (release.MIGRATIONS / dialect).glob('*.sql')}
            packaged = {p.name: p.read_bytes() for p in (self.output / 'app/migrations' / dialect).glob('*.sql')}
            self.assertTrue(expected)
            self.assertEqual(expected, packaged, 'The runtime must receive source-matching migrations')
        self.assertNotIn('ENTRYPOINT', dockerfile)
        self.assertEqual((self.output / 'ui/Dockerfile').read_bytes(),
                         (release.UI_SOURCE / 'Dockerfile').read_bytes())
        self.assertFalse(list(self.output.rglob('.env')))
        entries = (self.output / 'SHA256SUMS').read_text().splitlines()
        recorded = set()
        for entry in entries:
            expected, relative = entry.split('  ', 1)
            recorded.add(relative)
            self.assertEqual(release.sha256(self.output / relative), expected)
        actual = {p.relative_to(self.output).as_posix() for p in self.output.rglob('*')
                  if p.is_file() and p.name != 'SHA256SUMS'}
        self.assertEqual(recorded, actual)
        self.assertTrue(set(release.SCRIPTS).issubset(recorded))

    def test_rejects_requested_commit_different_from_build(self):
        with self.assertRaisesRegex(ValueError, 'source revision'):
            self.prepare(OTHER_REVISION)
        self.assertFalse(self.output.exists())

    def test_rejects_receipt_that_does_not_match_embedded_binary_commit(self):
        self.write_binary(OTHER_REVISION)
        with self.assertRaisesRegex(ValueError, 'embedded build metadata'):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_rejects_missing_anydoc_tag(self):
        path = self.backend / 'go-build-info.txt'
        path.write_text(path.read_text().replace('-tags=anydoc', '-tags=other'))
        with self.assertRaisesRegex(ValueError, 'anydoc tag'):
            self.prepare()

    def test_rejects_changed_binary_checksum(self):
        with (self.backend / 'WeKnora').open('ab') as stream:
            stream.write(b'changed')
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.prepare()

    def test_rejects_unresolved_runtime_library(self):
        (self.backend / 'runtime-libraries.txt').write_text('libanydoc.so => not found\n')
        with self.assertRaisesRegex(ValueError, 'library resolution'):
            self.prepare()

    def test_rejects_empty_bundle(self):
        (self.dist / 'assets/main.js').write_text('')
        with self.assertRaisesRegex(ValueError, 'asset is empty'):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_rejects_unbuilt_html_without_assets(self):
        (self.dist / 'index.html').write_text('<title>SOLEX</title>')
        with self.assertRaisesRegex(ValueError, 'completed SOLEX'):
            self.prepare()

    def test_rejects_public_secret_file_and_symlink(self):
        secret = self.dist / '.env.production'
        secret.write_text('LOCAL_ONLY=never-copy\n')
        with self.assertRaisesRegex(ValueError, 'Non-public file'):
            self.prepare()
        secret.unlink()
        secret.symlink_to(self.backend / 'go-build-info.txt')
        with self.assertRaisesRegex(ValueError, 'symlinks'):
            self.prepare()

    def test_refuses_to_overwrite_existing_release(self):
        self.output.mkdir()
        (self.output / 'keep.txt').write_text('keep this release')
        with self.assertRaisesRegex(ValueError, 'new destination'):
            self.prepare()
        self.assertEqual((self.output / 'keep.txt').read_text(), 'keep this release')


if __name__ == '__main__':
    unittest.main()
