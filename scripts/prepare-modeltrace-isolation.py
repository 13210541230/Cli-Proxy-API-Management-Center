"""Prepare owned, isolated CPA validation profiles without changing existing Suite assets."""
import argparse
import datetime as dt
import hashlib
import json
import os
import secrets
import shutil
import socket
import subprocess
from pathlib import Path

import yaml

SUITE = Path('D:/C_projects/CLIProxyAPI-Suite_7.3.12_windows_amd64')
SOURCE = Path('D:/C_projects/CLIProxyAPI-Suite_7.3.32_windows_amd64/config.yaml')
ROOT = SUITE / 'modeltrace-validation'
PROJECT = Path(__file__).resolve().parent.parent
PORT = 18327
MARKER = ROOT / '.modeltrace-validation-owner.json'


def prepare(mode: str, binary: str | None):
    with socket.socket() as probe:
        probe.settimeout(1)
        if probe.connect_ex(('127.0.0.1', PORT)) == 0:
            raise SystemExit('Port 18327 is occupied; stop the owned test instance before preparing any profile.')
    if binary and not Path(binary).is_file():
        raise SystemExit('The feature binary does not exist; build the backend first.')
    if ROOT.exists() and not MARKER.is_file():
        raise SystemExit('Refusing to modify an existing directory without our ownership marker.')
    ROOT.mkdir(parents=True, exist_ok=True)
    MARKER.write_text(json.dumps({'project': str(PROJECT), 'purpose': 'isolated ModelTrace validation'}), encoding='utf-8')
    dest = ROOT / mode
    dest.mkdir(exist_ok=True)
    if os.name == 'nt':
        identity = os.environ['USERDOMAIN'] + '\\' + os.environ['USERNAME']
        subprocess.run(['icacls', str(dest), '/inheritance:r', '/grant:r',
                        identity + ':(OI)(CI)F', 'SYSTEM:(OI)(CI)F'], check=True, capture_output=True)
    key_file = dest / 'management-key.txt'
    if not key_file.exists():
        key_file.write_text(secrets.token_urlsafe(32), encoding='utf-8')
    management_key = key_file.read_text(encoding='utf-8').strip()
    for folder in ('auth', 'static', 'logs', 'data'):
        (dest / folder).mkdir(parents=True, exist_ok=True)
    config = {
        'host': '127.0.0.1', 'port': PORT,
        'remote-management': {
            'allow-remote': False, 'secret-key': management_key,
            'disable-control-panel': False, 'disable-auto-update-panel': True,
        },
        'auth-dir': str(dest / 'auth'), 'api-keys': ['modeltrace-isolated-client'],
        'logging-to-file': False, 'usage-statistics-enabled': False,
        'request-retry': 0, 'max-retry-credentials': 0,
        'pprof': {'enable': False}, 'lan-discovery': {'enabled': False},
    }
    selected = []
    if mode == 'local':
        config['codex'] = {'base-url': 'http://127.0.0.1:18328', 'disable-codex-cloaking': True}
        for label, disabled in [('a', False), ('b', False), ('disabled', True)]:
            name = f'codex-isolated-{label}.json'
            value = {
                'type': 'codex', 'access_token': f'isolated-synthetic-token-{label}',
                'account_id': f'isolated-account-{label}', 'email': f'{label}@isolated.invalid',
                'base_url': 'http://127.0.0.1:18328', 'disabled': disabled,
                'expired': '2099-01-01T00:00:00Z',
            }
            (dest / 'auth' / name).write_text(json.dumps(value), encoding='utf-8')
            selected.append(name)
    else:
        source = yaml.safe_load(SOURCE.read_text(encoding='utf-8-sig'))
        auth_root = Path(source['auth-dir']).expanduser()
        if not auth_root.is_absolute():
            auth_root = SOURCE.parent / auth_root
        active = []
        for file in auth_root.glob('*.json'):
            try:
                value = json.loads(file.read_text(encoding='utf-8-sig'))
            except (OSError, ValueError):
                continue
            if value.get('type') == 'codex' and not value.get('disabled') and value.get('access_token'):
                active.append((file, value))
        if len(active) != 1:
            raise SystemExit('Real profile requires exactly one active Codex credential; select one explicitly before continuing.')
        file, value = active[0]
        if any(existing.name != file.name for existing in (dest / 'auth').glob('*.json')):
            raise SystemExit('Real profile contains additional credentials; refusing to extend the single-account test scope.')
        expiry = dt.datetime.fromisoformat(value['expired'].replace('Z', '+00:00'))
        if expiry <= dt.datetime.now(dt.timezone.utc) + dt.timedelta(minutes=15):
            raise SystemExit('Refusing to copy an expiring test credential that could trigger a refresh/rotation.')
        for key in ('proxy-url', 'codex', 'oauth-model-alias', 'oauth-excluded-models', 'payload'):
            if key in source:
                config[key] = source[key]
        # Use the current access token only. A copied refresh token could rotate
        # the shared upstream credential even though its source file is untouched.
        isolated_value = {key: item for key, item in value.items() if key != 'refresh_token'}
        (dest / 'auth' / file.name).write_text(json.dumps(isolated_value), encoding='utf-8')
        snapshot = {
            'config': str(SOURCE), 'config_sha256': hashlib.sha256(SOURCE.read_bytes()).hexdigest(),
            'auth': str(file), 'auth_sha256': hashlib.sha256(file.read_bytes()).hexdigest(),
        }
        (dest / '.source-integrity.json').write_text(json.dumps(snapshot), encoding='utf-8')
        selected.append(file.name)
    (dest / 'config.yaml').write_text(yaml.safe_dump(config, allow_unicode=True, sort_keys=False), encoding='utf-8')
    panel = PROJECT / 'dist' / 'index.html'
    if panel.is_file():
        shutil.copy2(panel, dest / 'static' / 'management.html')
    if binary:
        shutil.copy2(binary, dest / 'cli-proxy-api-modeltrace.exe')
    print(json.dumps({'directory': str(dest), 'port': PORT, 'mode': mode,
                      'credential_count': len(selected), 'binary_ready': (dest / 'cli-proxy-api-modeltrace.exe').is_file(),
                      'panel_ready': (dest / 'static' / 'management.html').is_file()}, ensure_ascii=False))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=['local', 'live'], required=True)
    parser.add_argument('--binary')
    args = parser.parse_args()
    prepare(args.mode, args.binary)
