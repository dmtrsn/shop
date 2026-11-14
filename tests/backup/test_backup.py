#!/usr/bin/env python3
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / 'scripts/backup-production.py'


class BackupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='shop-backup-test-')
        self.base = Path(self.temp.name)
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        shutil.copy(REPO / 'tests/backup/fake-docker.py', self.bin / 'docker')
        (self.bin / 'docker').chmod(0o700)
        (self.base / 'compose.yml').write_text('fixture')
        (self.base / 'state.json').write_text('{"running": true}')
        self.root = self.base / 'backups'
        self.lock = self.base / 'operation.lock'
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'], BACKUP_TEST_DIR=str(self.base))
        self.args = ['--environment', 'test', '--compose-file', str(self.base / 'compose.yml'),
                     '--project', 'test', '--backup-root', str(self.root), '--database-service', 'postgres',
                     '--database-name', 'shop', '--database-user', 'shop', '--writer-service', 'app',
                     '--media-root', '/media', '--application-git-sha', 'a' * 40, '--lock-file', str(self.lock),
                     '--ack-external-writers-stopped']

    def tearDown(self):
        self.temp.cleanup()

    def call(self, failure='', args=None, inject=''):
        env = dict(self.env, BACKUP_TEST_FAILURE=failure)
        if inject:
            # A test-only Python startup hook simulates filesystem/hash failures in
            # the real entrypoint; the production script has no injection flags.
            (self.base / 'sitecustomize.py').write_text(inject)
            env['PYTHONPATH'] = str(self.base)
        return subprocess.run([str(SCRIPT)] + (self.args if args is None else args), env=env,
                              capture_output=True, text=True, timeout=20)

    def calls(self):
        p = self.base / 'calls'
        return [json.loads(x) for x in p.read_text().splitlines()] if p.exists() else []

    def markers(self):
        return list(self.root.rglob('LOCAL_COMPLETE')) if self.root.exists() else []

    def check_no_stop(self, result):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(any(x[0] == 'stop' for x in self.calls()))
        self.assertFalse(self.markers())

    def test_missing_argument_and_ack(self):
        self.check_no_stop(self.call(args=self.args[:-1]))
        self.check_no_stop(self.call(args=[]))

    def test_unsafe_destination_and_inside_media(self):
        args = self.args.copy()
        args[args.index('--backup-root') + 1] = str(self.base / 'mediavol' / 'backups')
        self.check_no_stop(self.call(args=args))
        self.assertFalse((self.base / 'mediavol').exists())
        args[args.index('--backup-root') + 1] = '/'
        self.check_no_stop(self.call(args=args))

    def test_media_mismatch_and_disk(self):
        self.check_no_stop(self.call('mount'))
        self.check_no_stop(self.call('mountabsent'))
        self.check_no_stop(self.call('nested'))
        self.check_no_stop(self.call('disk'))

    def test_lock_contention(self):
        with self.lock.open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.check_no_stop(self.call())

    def test_stop_failure_never_copies(self):
        for mode in ('stop', 'refuse'):
            with self.subTest(mode=mode):
                result = self.call(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any('pg_dump' in x and '--version' not in x for x in self.calls()))
                self.assertFalse(any(x[0] == 'run' and 'tar' in x and '--help' not in x for x in self.calls()))
                self.assertFalse(self.markers())

    def test_artifact_failures_resume(self):
        for mode in ('dump', 'tar', 'metadata'):
            with self.subTest(mode=mode):
                (self.base / 'state.json').write_text('{"running": true}')
                result = self.call(mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.markers())
                self.assertTrue(json.loads((self.base / 'state.json').read_text())['running'])

    def test_checksum_manifest_and_publish_failures(self):
        hooks = [
            "import sys\nif sys.argv[0].endswith('backup-production.py'):\n import hashlib\n def fail(*a, **kw): raise OSError('injected checksum failure')\n hashlib.sha256=fail\n",
            "import sys\nif sys.argv[0].endswith('backup-production.py'):\n import json\n def fail(*a, **kw): raise OSError('injected manifest failure')\n json.dump=fail\n",
            "import sys\nif sys.argv[0].endswith('backup-production.py'):\n import ctypes\n ctypes.CDLL=lambda *a, **kw: object()\n",
        ]
        for hook in hooks:
            with self.subTest(hook=hook):
                (self.base / 'state.json').write_text('{"running": true}')
                result = self.call(inject=hook)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertFalse(self.markers())
                self.assertTrue(json.loads((self.base / 'state.json').read_text())['running'])

    def test_success_order_manifest_checksum_and_secrets(self):
        result = self.call()
        self.assertEqual(result.returncode, 0, result.stderr)
        markers = self.markers()
        self.assertEqual(len(markers), 1)
        final = markers[0].parent
        self.assertFalse(final.name.endswith('.partial'))
        manifest = json.loads((final / 'manifest.json').read_text())
        for field in ('formatVersion', 'backupId', 'environment', 'createdAtUtc', 'quiesceStartedAtUtc',
                      'quiesceEndedAtUtc', 'applicationGitSha', 'applicationImageDigest', 'database', 'media',
                      'representativeCounts', 'localCompletion', 'offHost'):
            self.assertIn(field, manifest)
        self.assertEqual(manifest['offHost'], {'status': 'not_verified', 'required': True})
        self.assertEqual(manifest['localCompletion']['status'], 'complete')
        for line in (final / 'SHA256SUMS').read_text().splitlines():
            digest, name = line.split('  ')
            self.assertEqual(digest, hashlib.sha256((final / name).read_bytes()).hexdigest())
        self.assertNotIn('private-secret', (final / 'manifest.json').read_text() + result.stdout + result.stderr + (self.base / 'calls').read_text())
        calls = self.calls()
        stop = next(i for i, x in enumerate(calls) if x[0] == 'stop')
        dump = next(i for i, x in enumerate(calls) if 'pg_dump' in x and '--version' not in x)
        tar = next(i for i, x in enumerate(calls) if x[0] == 'run' and 'tar' in x and '--help' not in x)
        start = next(i for i, x in enumerate(calls) if x[0] == 'start')
        self.assertLess(stop, dump)
        self.assertLess(dump, tar)
        self.assertLess(tar, start)
        self.assertIn(['marker-at-start', True], calls[start + 1:])
        self.assertTrue(any(x[:2] == ['container', 'inspect'] and x[2] == 'appid' for x in calls[stop + 1:dump]))
        self.assertTrue(any(x[:2] == ['container', 'inspect'] and x[2] == 'appid' for x in calls[start + 1:]))
        self.assertEqual(calls[stop][1:3], ['--time', '30'])
        self.assertEqual(final.stat().st_mode & 0o777, 0o700)

    def test_initially_stopped(self):
        (self.base / 'state.json').write_text('{"running": false}')
        result = self.call()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(x[0] in ('stop', 'start') for x in self.calls()))
        self.assertEqual(len(self.markers()), 1)

    def test_restart_failure_preserves_marker(self):
        result = self.call('restart')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('OPERATIONAL FAILURE', result.stderr)
        self.assertIn('LOCAL COMPLETE', result.stderr)
        self.assertEqual(len(self.markers()), 1)


if __name__ == '__main__':
    unittest.main()
