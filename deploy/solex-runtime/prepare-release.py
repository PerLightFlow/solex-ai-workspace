#!/usr/bin/env python3
"""Package already-built SOLEX artifacts. No builds, service changes, or credentials."""
import argparse
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import shlex
import shutil
from urllib.parse import urlsplit, unquote

SOURCE = Path(__file__).resolve().parent
UI_SOURCE = SOURCE.parent / 'solex-ui'
MIGRATIONS = SOURCE.parent.parent / 'weknora' / 'migrations'
CONFIG = SOURCE.parent.parent / 'weknora' / 'config'
PROMPT_FILES = (
    'system_prompt.yaml', 'context_template.yaml', 'rewrite.yaml', 'fallback.yaml',
    'generate_session_title.yaml', 'generate_summary.yaml', 'generate_kb_description.yaml',
    'keywords_extraction.yaml', 'agent_system_prompt.yaml', 'graph_extraction.yaml',
    'generate_questions.yaml', 'intent_prompts.yaml',
)
APP_RUNTIME = 'wechatopenai/weknora-app@sha256:25b20959b7456f7842ef55ed4feff3694dbd0c6f8af4117c1fb3c5dec6bdf14d'
UI_RUNTIME = 'wechatopenai/weknora-ui@sha256:df26705d866997db07f3da78dabedc4f594b383566a3996810c166a5c6bee393'
SCRIPTS = ('apply.sh', 'deploy.py', 'api_client.py', 'acceptance.py', 'stream_check.py', 'attribution_check.py', 'runtime_settings_check.py',
           'file_generation_setup.py', 'file_generation_check.py', 'compose.file-generation.yaml',
           'docker_control.py', 'account_handover_check.py', 'invite_picker_check.py', 'migrate-user-spaces.py', 'rehearse-user-spaces.py', 'spaces_check.py', 'deploy-user-spaces.py')
EVIDENCE = ('go-build-info.txt', 'runtime-libraries.txt', 'WeKnora.sha256')


class Page(HTMLParser):
    def __init__(self):
        super().__init__()
        self.title = ''
        self.in_title = False
        self.assets = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'title': self.in_title = True
        if tag == 'script' and attrs.get('src'): self.assets.append(attrs['src'])
        if tag == 'link' and attrs.get('rel') in ('stylesheet', 'modulepreload') and attrs.get('href'):
            self.assets.append(attrs['href'])

    def handle_endtag(self, tag):
        if tag == 'title': self.in_title = False

    def handle_data(self, text):
        if self.in_title: self.title += text


def plain_file(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.resolve() != path.absolute():
        raise ValueError('Artifact must be a regular file without symlinks: ' + path.name)
    return path


def sha256(path):
    digest = hashlib.sha256()
    with plain_file(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''): digest.update(block)
    return digest.hexdigest()


def validate_backend(directory, revision):
    directory = Path(directory).absolute()
    binary = plain_file(directory / 'WeKnora')
    info = plain_file(directory / 'go-build-info.txt').read_text()
    libraries = plain_file(directory / 'runtime-libraries.txt').read_text()
    checksum = plain_file(directory / 'WeKnora.sha256').read_text().strip()
    match = re.fullmatch(r'([0-9a-f]{64})\s+\*?([^\r\n]+)', checksum)
    if not match or Path(match[2]).name != 'WeKnora' or match[1] != sha256(binary):
        raise ValueError('Backend binary checksum does not match its build evidence')
    with binary.open('rb') as stream:
        if stream.read(4) != b'\x7fELF': raise ValueError('Backend must be a Linux ELF binary')
    if not re.search(r'\bgithub\.com/Tencent/WeKnora/internal/handler\.CommitID=' + revision + r"(?=['\"\s]|$)", info):
        raise ValueError('Go build evidence does not contain the requested full source revision')
    tags_line = re.search(r'(?m)^\s*build\s+-tags=(.+)$', info)
    tags = shlex.split(tags_line[1])[0].split(',') if tags_line else []
    if 'anydoc' not in tags: raise ValueError('Backend was not built with the anydoc tag')
    if not libraries.strip() or 'not found' in libraries.lower():
        raise ValueError('Runtime library resolution is missing or incomplete')
    # Match the evidence to the actual Go binary, not just an editable text receipt.
    import mmap
    with binary.open('rb') as stream, mmap.mmap(stream.fileno(), 0, access=mmap.ACCESS_READ) as data:
        if data.find(('CommitID=' + revision).encode()) < 0 or data.find(b'-tags=anydoc') < 0:
            raise ValueError('Binary embedded build metadata does not match the evidence')
    return match[1]


def validate_dist(directory):
    directory = Path(directory).absolute()
    parser = Page()
    parser.feed(plain_file(directory / 'index.html').read_text())
    if not parser.title.strip().startswith('SOLEX') or not parser.assets:
        raise ValueError('A completed SOLEX frontend bundle is required')
    has_script = False
    for asset in parser.assets:
        parsed = urlsplit(asset)
        if parsed.scheme or parsed.netloc: raise ValueError('Frontend build references a remote executable asset')
        relative = unquote(parsed.path).lstrip('/')
        if '..' in Path(relative).parts: raise ValueError('Frontend asset escapes its release directory')
        path = plain_file(directory / relative)
        if not path.stat().st_size: raise ValueError('Frontend asset is empty')
        has_script = has_script or path.suffix == '.js'
    if not has_script: raise ValueError('Frontend build has no JavaScript bundle')
    digest = hashlib.sha256()
    for path in sorted(directory.rglob('*')):
        if path.is_symlink(): raise ValueError('Frontend must not contain symlinks')
        if path.is_dir(): continue
        plain_file(path)
        name = path.name.lower()
        if name.startswith('.env') or name in ('admin.json', 'credentials.json', 'login.json') or path.suffix.lower() in ('.pem', '.key', '.p12', '.pfx') or any(part in ('.git', 'node_modules') for part in path.relative_to(directory).parts):
            raise ValueError('Non-public file found in frontend artifacts')
        if path.suffix.lower() in ('.js', '.json', '.html', '.txt', '.css', '.map'):
            data = path.read_bytes()
            if re.search(rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|ark-[a-f0-9]{8}-[a-f0-9-]{24,}', data):
                raise ValueError('Potential private credential found in frontend artifacts')
        digest.update(path.relative_to(directory).as_posix().encode() + b'\0')
        digest.update(bytes.fromhex(sha256(path)))
    return digest.hexdigest()


def prepare(backend_dir, dist, destination, revision, schema_before=136):
    if schema_before not in (136, 137):
        raise ValueError("Only the 136-to-137 migration or a schema-137 update can be packaged")
    if not re.fullmatch(r'[a-f0-9]{40}', revision):
        raise ValueError('Use the full 40-character source commit')
    backend_dir, dist = Path(backend_dir).absolute(), Path(dist).absolute()
    destination = Path(destination).absolute()
    if destination.exists() or destination.is_symlink() or destination.parent.resolve() != destination.parent.absolute():
        raise ValueError('Choose a new destination under an existing regular parent directory')
    binary_hash = validate_backend(backend_dir, revision)
    dist_hash = validate_dist(dist)
    for name in SCRIPTS: plain_file(SOURCE / name)
    for name in ('Dockerfile', '50x.html', '.dockerignore'): plain_file(UI_SOURCE / name)
    # Ship public prompt definitions with the source, not the upstream image's
    # defaults. Deliberately exclude config.yaml and local model/secret files.
    config_files = [plain_file(CONFIG / 'builtin_agents.yaml')]
    config_files += [plain_file(CONFIG / 'prompt_templates' / name) for name in PROMPT_FILES]
    migration_files = []
    for dialect in ('versioned', 'sqlite'):
        scripts = sorted((MIGRATIONS / dialect).glob('*.sql'))
        if not scripts:
            raise ValueError('Database migration scripts are missing: ' + dialect)
        for script in scripts:
            plain_file(script)
            if not re.fullmatch(r'[0-9]{6}_[a-zA-Z0-9_]+\.(up|down)\.sql', script.name):
                raise ValueError('Unexpected migration filename')
            migration_files.append(script)
    for dialect, expected in (('versioned', 137), ('sqlite', 54)):
        versions = [int(p.name[:6]) for p in migration_files if p.parent.name == dialect]
        if max(versions) != expected:
            raise ValueError('Reassess schema rollback compatibility before packaging changed migrations')
        plain_file(MIGRATIONS / dialect / f'{expected:06d}_user_spaces.up.sql')
    suffix = revision[:12]
    metadata = {
        'release_id': 'solex-runtime-' + suffix,
        'source_revision': revision,
        'app_image': 'solex/solex-app:' + suffix,
        'ui_image': 'solex/solex-ui:managed-' + suffix,
        'app_runtime': APP_RUNTIME,
        'ui_runtime': UI_RUNTIME,
        'backend_sha256': binary_hash,
        'dist_sha256': dist_hash,
        'build_tags': ['anydoc'],
        # Permission migration is not compatible with the legacy authorization code.
        'authorization_model': 'account_spaces_v1',
        'postgres_schema_before': schema_before,
        'postgres_schema_after': 137,
    }
    destination.mkdir(mode=0o700)
    app, ui, evidence = destination / 'app', destination / 'ui', destination / 'build-evidence'
    app.mkdir(); ui.mkdir(); evidence.mkdir()
    # Explicit allow-lists prevent backend build directories or local deployment
    # state from slipping into either Docker build context.
    for name in SCRIPTS: shutil.copy2(SOURCE / name, destination / name)
    for name in EVIDENCE: shutil.copy2(backend_dir / name, evidence / name)
    shutil.copy2(backend_dir / 'WeKnora', app / 'WeKnora')
    os.chmod(app / 'WeKnora', 0o755)
    for script in migration_files:
        target = app / 'migrations' / script.relative_to(MIGRATIONS)
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(script, target)
    for path in config_files:
        target = app / 'config' / path.relative_to(CONFIG)
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)
    (app / '.dockerignore').write_text('*\n!Dockerfile\n!WeKnora\n!migrations/\n!migrations/**\n!config/\n!config/**\n')
    (app / 'Dockerfile').write_text(
        'FROM ' + APP_RUNTIME + '\nARG RELEASE_ID\nARG SOURCE_REVISION\n'
        'LABEL org.opencontainers.image.title="SOLEX" \\\n'
        '      org.opencontainers.image.revision="${SOURCE_REVISION}" \\\n'
        '      io.solex.runtime.release="${RELEASE_ID}"\n'
        'COPY WeKnora /app/WeKnora\n'
        'COPY migrations/ /app/migrations/\n'
        'COPY config/ /app/config/\n')
    for name in ('Dockerfile', '50x.html', '.dockerignore'): shutil.copy2(UI_SOURCE / name, ui / name)
    shutil.copytree(dist, ui / 'dist')
    (destination / 'release.json').write_text(json.dumps(metadata, indent=2) + '\n')
    files = sorted(path for path in destination.rglob('*') if path.is_file())
    (destination / 'SHA256SUMS').write_text(''.join(
        sha256(path) + '  ' + path.relative_to(destination).as_posix() + '\n' for path in files))
    return metadata


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backend-dir', required=True)
    parser.add_argument('--dist', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--source-revision', required=True)
    parser.add_argument('--schema-before', type=int, choices=(136, 137), default=136)
    args = parser.parse_args()
    metadata = prepare(args.backend_dir, args.dist, args.output, args.source_revision, args.schema_before)
    print(json.dumps({'release_id': metadata['release_id'], 'directory': str(Path(args.output).absolute()),
                      'backend_sha256': metadata['backend_sha256'], 'dist_sha256': metadata['dist_sha256']}, indent=2))


if __name__ == '__main__':
    try: main()
    except (OSError, ValueError) as exc:
        raise SystemExit(str(exc)) from None
