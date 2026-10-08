"""Isolation preparation tests use only temporary synthetic credentials."""
import contextlib
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml

PROJECT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('prepare_isolation', PROJECT / 'scripts' / 'prepare-modeltrace-isolation.py')
isolation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(isolation)


class IsolationPreparationTest(unittest.TestCase):
    def setUp(self):
        parent = PROJECT / 'build_tmp'
        parent.mkdir(exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix='modeltrace-isolation-', dir=parent)
        self.base = Path(self.temp.name)
        self.suite = self.base / 'suite'
        self.suite.mkdir()
        self.root = self.suite / 'modeltrace-validation'
        self.source = self.base / 'source.yaml'
        self.auth = self.base / 'source-auth'
        self.auth.mkdir()
        self.original = self.suite / 'config.yaml'
        self.original.write_text('DO NOT CHANGE THIS SUITE CONFIG', encoding='utf-8')
        self.patches = [patch.object(isolation, name, value) for name, value in {
            'ROOT': self.root, 'SUITE': self.suite, 'SOURCE': self.source,
            'MARKER': self.root / '.modeltrace-validation-owner.json',
            'PROJECT': self.base / 'project',
        }.items()]
        for item in self.patches:
            item.start()
        socket_patch = patch.object(isolation.socket, 'socket')
        self.socket = socket_patch.start().return_value.__enter__.return_value
        self.socket.connect_ex.return_value = 111
        self.patches.append(socket_patch)

    def tearDown(self):
        for item in reversed(self.patches):
            item.stop()
        self.temp.cleanup()
        try:
            (PROJECT / 'build_tmp').rmdir()
        except OSError:
            pass

    def run_prepare(self, mode):
        with contextlib.redirect_stdout(io.StringIO()):
            isolation.prepare(mode, None)

    def make_source(self):
        self.source.write_text(yaml.safe_dump({
            'port': 18317, 'auth-dir': str(self.auth),
            'codex': {'disable-codex-cloaking': True},
            'remote-management': {'secret-key': 'do-not-copy-source-key'},
            'codex-api-key': [{'api-key': 'do-not-copy-other-credentials'}],
        }), encoding='utf-8')
        for name, disabled in [('selected', False), ('disabled', True)]:
            (self.auth / f'{name}.json').write_text(json.dumps({
                'type': 'codex', 'disabled': disabled,
                'access_token': 'synthetic-only', 'refresh_token': 'do-not-rotate-shared-token',
                'expired': '2099-01-01T00:00:00Z',
            }), encoding='utf-8')

    def test_local_uses_owned_subdirectory_and_does_not_touch_suite(self):
        self.run_prepare('local')
        config = yaml.safe_load((self.root / 'local' / 'config.yaml').read_text(encoding='utf-8'))
        self.assertEqual(config['host'], '127.0.0.1')
        self.assertEqual(config['port'], 18327)
        self.assertEqual(config['remote-management']['secret-key'],
                         (self.root / 'local' / 'management-key.txt').read_text(encoding='utf-8'))
        self.assertGreater(len(config['remote-management']['secret-key']), 30)
        self.assertEqual(config['auth-dir'], str(self.root / 'local' / 'auth'))
        self.assertEqual(config['codex']['base-url'], 'http://127.0.0.1:18328')
        self.assertEqual(len(list((self.root / 'local' / 'auth').glob('*.json'))), 3)
        self.assertEqual(self.original.read_text(encoding='utf-8'), 'DO NOT CHANGE THIS SUITE CONFIG')

    def test_running_instance_is_not_modified(self):
        self.socket.connect_ex.return_value = 0
        with self.assertRaises(SystemExit):
            self.run_prepare('local')
        self.assertFalse(self.root.exists())

    def test_existing_unowned_directory_is_refused(self):
        self.root.mkdir()
        with self.assertRaises(SystemExit):
            self.run_prepare('local')
        self.assertFalse((self.root / 'local').exists())

    def test_live_copies_only_one_active_credential_and_preserves_source(self):
        self.make_source()
        before = {p: p.read_bytes() for p in [self.source, *self.auth.glob('*.json')]}
        self.run_prepare('live')
        self.assertEqual([p.name for p in (self.root / 'live' / 'auth').glob('*.json')], ['selected.json'])
        copied = json.loads((self.root / 'live' / 'auth' / 'selected.json').read_text(encoding='utf-8'))
        self.assertEqual(copied['access_token'], 'synthetic-only')
        self.assertNotIn('refresh_token', copied)
        config = yaml.safe_load((self.root / 'live' / 'config.yaml').read_text(encoding='utf-8'))
        self.assertNotIn('codex-api-key', config)
        self.assertNotEqual(config['remote-management']['secret-key'], 'do-not-copy-source-key')
        self.assertEqual(config['request-retry'], 0)
        for file, content in before.items():
            self.assertEqual(file.read_bytes(), content)
        self.assertTrue((self.root / 'live' / '.source-integrity.json').is_file())

    def test_live_refuses_multiple_active_credentials(self):
        self.make_source()
        (self.auth / 'another.json').write_bytes((self.auth / 'selected.json').read_bytes())
        with self.assertRaises(SystemExit):
            self.run_prepare('live')
        self.assertEqual(list((self.root / 'live' / 'auth').glob('*.json')), [])


if __name__ == '__main__':
    unittest.main()
