#!/usr/bin/env python3
"""Deterministic command-boundary tests; these are NOT real Docker evidence."""
import copy
import hashlib
import importlib.util
import io
import json
import subprocess
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / 'scripts'))
from restore_drill_integrity import IntegrityError, archive_inventory, verify_media, legacy_path

spec = importlib.util.spec_from_file_location('drill', REPO / 'scripts/restore-production-drill.py')
drill = importlib.util.module_from_spec(spec)
spec.loader.exec_module(drill)
SHA = '1' * 40
ID = 'med_' + 'a' * 32
ORIGINAL = b'fixture-original-A'
THUMB = b'fixture-thumbnail-A'
THUMB_SHA = hashlib.sha256(THUMB).hexdigest()
ORIGINAL_PATH = f'objects/aa/{ID}'
THUMB_PATH = f'thumbnails/aa/{ID}/{THUMB_SHA}'


def tar_bytes(files=None, entries=None):
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w') as archive:
        for name, content in (files or {}).items():
            member = tarfile.TarInfo(name)
            member.size = len(content)
            member.mode = 0o640
            archive.addfile(member, io.BytesIO(content))
        for member in entries or []:
            archive.addfile(member)
    return out.getvalue()


def snapshot():
    return {'counts': {name: 1 for name in drill.COUNTS}, 'migrationMarker': [{'version': '022_media_thumbnails', 'outcome': 'applied'}],
            'postgresVersionNum': '160004', 'extensions': ['pg_trgm', 'plpgsql'],
            'assets': [{'id': ID, 'size_bytes': len(ORIGINAL), 'sha256': hashlib.sha256(ORIGINAL).hexdigest(), 'deleted': False}],
            'thumbnails': [{'media_id': ID, 'size_bytes': len(THUMB), 'sha256': THUMB_SHA}],
            'productMedia': [{'product_id': 'product-a', 'media_id': ID, 'role': 'primary', 'position': 0},
                             {'product_id': 'product-a', 'media_id': ID, 'role': 'gallery', 'position': 0}],
            'products': [{'id': 'product-a', 'image': '/assets/fixture.png', 'gallery': [], 'archived': False, 'legacyRetired': False}],
            'historicalImages': ['/media/' + ID, '/assets/fixture.png']}


class FakeDocker:
    """Implements only the emitted command boundary; unknown commands fail.

    pg_restore must read the actual backup stdin. Its source state is changed
    to B first and is never used to produce the restored snapshot or media.
    """
    def __init__(self, test, failure=''):
        self.test, self.failure = test, failure
        self.calls, self.reads = [], []
        self.model = None
        self.restored = None
        self.media = None
        self.source = snapshot()
        self.source['counts']['products'] = 99  # state B AFTER the backup
        self.source['historicalImages'] = ['/assets/state-b.png']
        self.source_media = b'source-media-state-B'
        self.token = None
        self.app_started = False

    def inspect(self, kind, ident):
        a = self.test.args
        if kind == 'image':
            return {'Id': 'sha256:' + ('a' if ident == 'app-image' else 'b') * 64,
                    'Config': {'Labels': {'org.opencontainers.image.revision': SHA}}, 'RepoDigests': []}
        if kind == 'volume':
            source = ident in ('source_database', 'source_media')
            mountpoint = '/var/lib/docker/volumes/' + ident + '/_data'
            if not source and self.failure == 'alias':
                mountpoint = '/var/lib/docker/volumes/source_media/_data'
            if source and self.failure == 'output-alias':
                mountpoint = str(self.test.base)
            return {'Name': ident, 'Driver': 'local', 'Options': {}, 'Mountpoint': mountpoint,
                    'Labels': {'com.docker.compose.project': 'source'} if source else {drill.LABEL: self.token}}
        if kind == 'network':
            publication = ident == a.project + '_publication'
            members = {'new-app': {}} if publication else {'new-db': {}}
            if not publication and self.app_started:
                members['new-app'] = {}
            if publication and self.failure == 'publication-participant':
                members['source-app'] = {}
            if publication and self.failure == 'db-on-publication':
                members['new-db'] = {}
            return {'Name': ident, 'Id': 'publication-network-id' if publication else 'new-network-id',
                    'Driver': 'bridge', 'Internal': not publication,
                    'Labels': {drill.LABEL: self.token}, 'Containers': members}
        if kind == 'container':
            service = 'app' if ident == 'new-app' else a.database_service
            app = service == 'app'
            networks = {a.project + '_' + key: {'NetworkID': 'publication-network-id' if key == 'publication' else 'new-network-id'}
                        for key in self.model['services'][service]['networks']}
            if app and self.failure == 'network-alias':
                networks[a.project + '_publication']['NetworkID'] = 'source-network-id'
            requested = {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': str(a.port)}]} if app else {}
            # A correct request on an internal-only topology is NOT published.
            publication = app and any(not self.model['networks'][key]['internal']
                                      for key in self.model['services'][service]['networks'])
            actual = copy.deepcopy(requested) if publication else {}
            if app and self.failure == 'unpublished':
                actual = {}
            if not app and self.failure == 'db-published':
                actual = {'5432/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '5432'}]}
            return {'Id': ident, 'Image': 'sha256:' + ('a' if app else 'b') * 64, 'State': {'Running': True},
                    'Config': {'Labels': {drill.LABEL: self.token, 'com.docker.compose.project': a.project,
                                         'com.docker.compose.service': service},
                               'Env': [k + '=' + v for k, v in self.model['services'][service]['environment'].items()]},
                    'Mounts': [{'Destination': '/var/lib/sweetshop/media' if app else '/var/lib/postgresql/data',
                                'Type': 'volume', 'Name': a.media_volume if app else a.database_volume}],
                    'HostConfig': {'PortBindings': requested},
                    'NetworkSettings': {'Networks': networks, 'Ports': actual}}
        raise AssertionError((kind, ident))

    def run(self, argv, *, stdin=None, stdout=None, timeout=180):
        self.calls.append(argv)
        if argv[0] == 'git':
            return SHA if 'rev-parse' in argv else ''
        self.test.assertEqual(argv[0], 'docker')
        if argv[1:3] == ['context', 'inspect']:
            if self.failure == 'denied':
                raise IntegrityError('Command failed: docker')
            return json.dumps([{'Endpoints': {'docker': {'Host': 'unix:///var/run/docker.sock'}}}])
        if argv[1] == 'info':
            return '26.0'
        if argv[1:3] == ['volume', 'ls']:
            volumes = ['source_database', 'source_media']
            if self.failure == 'existing':
                volumes.append(self.test.args.database_volume)
            if self.model:
                volumes += [self.test.args.database_volume, self.test.args.media_volume]
            return '\n'.join(volumes)
        if argv[1:3] == ['network', 'ls']:
            return self.test.args.project + '_publication' if self.failure == 'existing-publication' else ''
        if argv[1] == 'ps':
            return ''
        if argv[1] == 'compose':
            self.model = json.loads(Path(argv[argv.index('--file') + 1]).read_text())
            self.token = self.model['services']['app']['labels'][drill.LABEL]
            if 'config' in argv:
                return ''
            if 'up' in argv:
                if argv[-1] == 'app' and self.failure == 'app-start':
                    raise IntegrityError('Command failed: docker')
                if argv[-1] == 'app':
                    self.app_started = True
                return ''
            if 'ps' in argv:
                return 'new-app' if argv[-1] == 'app' else 'new-db'
        if argv[1] == 'exec':
            if 'pg_restore' in argv:
                if self.failure == 'pg_restore':
                    raise IntegrityError('Command failed: docker')
                self.restored = json.loads(stdin.read())
                if self.failure == 'counts':
                    self.restored['counts']['products'] += 1
                return ''
            self.test.assertIn('psql', argv)
            query = argv[-1]
            if query == 'SELECT 1':
                return '1'
            if query == 'SHOW server_version_num':
                return '160004'
            if query.startswith('SELECT count(*) FROM pg_class'):
                return '1' if self.failure == 'nonempty-db' else '0'
            if query.startswith('BEGIN READ ONLY; SELECT json_build_object'):
                return 'BEGIN\n' + json.dumps(self.restored) + '\nCOMMIT'
        if argv[1] == 'run':
            if 'id' in argv:
                return '10001'
            if stdin is not None:
                self.media = stdin.read()
                return ''
            if stdout is not None:
                stdout.write(self.media)
                return None
            if self.failure == 'nonempty-media' and 'test -z' in argv[-1]:
                raise IntegrityError('Command failed: docker')
            if self.failure == 'legacy' and argv[-1] == '/app/web/assets/fixture.png':
                raise IntegrityError('Command failed: docker')
            return ''
        raise AssertionError('Unexpected command: ' + repr(argv))

    def read(self, port, path, cookie='', expected=None):
        self.reads.append(path)
        if self.failure == 'http' and path == '/api/catalog':
            raise IntegrityError('Application read check failed')


class RestoreTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='shop-restore-tests-')
        self.base = Path(self.tmp.name)
        self.backup = self.base / 'backup'
        self.backup.mkdir()
        self.state_a = snapshot()
        self.files = {ORIGINAL_PATH: ORIGINAL, THUMB_PATH: THUMB, 'tmp/upload-leftover': b'harmless'}
        (self.backup / 'database.dump').write_text(json.dumps(self.state_a))
        (self.backup / 'media.tar').write_bytes(tar_bytes(self.files))
        self.manifest = {'formatVersion': 1, 'backupId': '20261115T000000Z-abcdef123456', 'environment': 'source',
                         'applicationGitSha': SHA, 'applicationImageDigest': None,
                         'legacyAssetDependency': 'Retain matching release and web/assets',
                         'database': {'name': 'source_db', 'postgresServerVersion': '16.4', 'dumpFilename': 'database.dump',
                                      'migrationMarker': self.state_a['migrationMarker']},
                         'media': {'archiveFilename': 'media.tar', 'sourceIdentity': 'source_media'},
                         'representativeCounts': self.state_a['counts'], 'localCompletion': {'status': 'complete'},
                         'offHost': {'status': 'not_verified', 'required': True}}
        self.reseal()
        (self.backup / 'LOCAL_COMPLETE').write_text('LOCAL COMPLETE; off-host copy not verified\n')
        self.session = self.base / 'sessions.json'
        self.session.write_text(json.dumps({'staffCookie': 'private-staff-secret', 'customerCookie': 'private-customer-secret'}))
        self.argv = ['--backup-dir', str(self.backup), '--environment', 'restore-test',
                     '--compose-file', str(self.base / 'work' / 'compose.json'), '--project', 'drill-fixture',
                     '--database-service', 'postgres', '--database-name', 'drilldb', '--database-user', 'drilluser',
                     '--database-volume', 'drill-fixture_database', '--media-volume', 'drill-fixture_media',
                     '--source-project', 'source', '--source-database-volume', 'source_database',
                     '--application-image', 'app-image', '--application-git-sha', SHA, '--postgres-image', 'pg-image',
                     '--report', str(self.base / 'report.json'), '--port', '38457', '--disposable',
                     '--read-sessions-file', str(self.session)]
        self.args = drill.parse_args(self.argv)

    def tearDown(self):
        self.tmp.cleanup()

    def reseal(self):
        for section, name, prefix in [('database', 'database.dump', 'dump'), ('media', 'media.tar', 'archive')]:
            self.manifest[section][prefix + 'Bytes'] = (self.backup / name).stat().st_size
            self.manifest[section][prefix + 'Sha256'] = drill.file_hash(self.backup / name)
        (self.backup / 'manifest.json').write_text(json.dumps(self.manifest))
        (self.backup / 'SHA256SUMS').write_text(''.join(drill.file_hash(self.backup / name) + '  ' + name + '\n'
                                                     for name in ('database.dump', 'media.tar', 'manifest.json')))

    def execute(self, failure='', argv=None):
        fake = FakeDocker(self, failure)
        with patch.object(drill, 'run', fake.run), patch.object(drill, 'inspect', fake.inspect), \
             patch.object(drill, 'preflight_resources', return_value={'test': True}), \
             patch.object(drill.socket, 'socket'), \
             patch.object(drill, 'legacy_http_check', return_value='PASS'), \
             patch.object(drill, 'read_check', fake.read), patch('builtins.print'):
            code = drill.main(argv or self.argv)
        return code, json.loads((self.base / 'report.json').read_text()), fake

    def test_valid_artifacts_and_mutate_after_backup_point(self):
        before = {p.name: (p.read_bytes(), p.stat().st_mtime_ns) for p in self.backup.iterdir()}
        code, report, fake = self.execute()
        self.assertEqual(code, 0, report)
        self.assertEqual(report['verdict'], 'PASS')
        self.assertEqual(fake.restored, self.state_a)
        self.assertNotEqual(fake.restored['counts'], fake.source['counts'])
        self.assertNotEqual(fake.restored['historicalImages'], fake.source['historicalImages'])
        self.assertNotEqual(fake.media, fake.source_media)
        self.assertEqual(fake.media, (self.backup / 'media.tar').read_bytes())
        self.assertEqual(before, {p.name: (p.read_bytes(), p.stat().st_mtime_ns) for p in self.backup.iterdir()})
        self.assertEqual(report['mediaIntegrity']['extras']['tmpUploadLeftovers'], 1)
        self.assertEqual(report['isolation']['afterStart'], 'PASS')
        self.assertEqual(report['countComparison']['actual'], self.state_a['counts'])
        self.assertGreaterEqual(report['durationSeconds'], 0)
        raw = json.dumps(report)
        self.assertNotIn('private-staff-secret', raw)
        self.assertNotIn('private-customer-secret', raw)
        self.assertNotIn('product-a', raw)
        self.assertNotIn('/assets/fixture.png', raw)
        self.assertEqual((self.base / 'report.json').stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.base / 'work' / 'compose.json').stat().st_mode & 0o777, 0o600)
        for call in fake.calls:
            self.assertFalse(any(x in call for x in ('dropdb', 'createdb', 'prune', 'down', '--apply')))
            if 'pg_restore' in call:
                self.assertIn('new-db', call)
                self.assertNotIn('source_db', call)
                self.assertIn('--exit-on-error', call)
                self.assertIn('--no-owner', call)
                self.assertNotIn('--clean', call)
            for argument in call:
                if argument.startswith('type=volume'):
                    self.assertNotIn('source_media', argument)
                    self.assertNotIn('source_database', argument)
        model = fake.model
        self.assertTrue(model['networks']['isolated']['internal'])
        self.assertFalse(model['networks']['publication']['internal'])
        self.assertEqual(model['services']['postgres']['networks'], ['isolated'])
        self.assertEqual(model['services']['app']['networks'], ['isolated', 'publication'])
        self.assertEqual(report['isolation']['target']['publicationNetworkId'], 'publication-network-id')
        self.assertNotIn('ports', model['services']['postgres'])
        self.assertEqual(model['services']['app']['environment']['RUN_MIGRATIONS'], 'false')
        self.assertEqual(model['services']['app']['environment']['EMAIL_TRANSPORT'], '')

    def test_requested_binding_without_runtime_publication_fails(self):
        code, report, fake = self.execute('unpublished')
        app = fake.inspect('container', 'new-app')
        self.assertEqual(app['HostConfig']['PortBindings'],
                         {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': str(self.args.port)}]})
        self.assertEqual(app['NetworkSettings']['Ports'], {})
        self.assertEqual((code, report['verdict']), (1, 'FAIL'))
        self.assertEqual(report['failure'], 'Unexpected bound application port')
        self.assertFalse(fake.reads)

    def test_runtime_publication_rejects_unsafe_or_extra_bindings(self):
        expected = {'HostIp': '127.0.0.1', 'HostPort': str(self.args.port)}
        requested = {'8080/tcp': [expected]}
        for ports in ({}, {'8080/tcp': None},
                      {'8080/tcp': [dict(expected, HostIp='0.0.0.0')]},
                      {'8080/tcp': [dict(expected, HostIp='::')]},
                      {'8080/tcp': [dict(expected, HostPort='12345')]},
                      {'8080/tcp': [expected, expected]},
                      dict(requested, **{'9090/tcp': [expected]})):
            with self.subTest(ports=ports), self.assertRaises(IntegrityError):
                drill.prove_loopback_publication(
                    {'HostConfig': {'PortBindings': requested}, 'NetworkSettings': {'Ports': ports}}, self.args.port)
        drill.prove_loopback_publication(
            {'HostConfig': {'PortBindings': requested}, 'NetworkSettings': {'Ports': requested}}, self.args.port)

    def test_fake_does_not_publish_internal_only_app(self):
        fake = FakeDocker(self)
        fake.model = drill.compose_model(self.args, 'token', 'app', 'pg', 'secret')
        fake.model['services']['app']['networks'] = ['isolated']
        app = fake.inspect('container', 'new-app')
        self.assertTrue(app['HostConfig']['PortBindings'])
        self.assertEqual(app['NetworkSettings']['Ports'], {})

    def test_artifact_failures_create_no_docker_resources(self):
        (self.backup / 'LOCAL_COMPLETE').unlink()
        code, report, fake = self.execute()
        self.assertEqual((code, report['verdict']), (1, 'FAIL'))
        self.assertFalse(fake.calls)
        self.assertFalse((self.base / 'work').exists())

    def test_cli_failure_report_with_no_docker_command_available(self):
        (self.backup / 'media.tar').write_bytes(b'corrupt')
        result = subprocess.run([sys.executable, str(REPO / 'scripts/restore-production-drill.py')] + self.argv,
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 1, result.stderr)
        report = json.loads((self.base / 'report.json').read_text())
        self.assertEqual(report['verdict'], 'FAIL')
        self.assertEqual(report['failure'], 'Artifact checksum mismatch')
        self.assertFalse((self.base / 'work').exists())

    def test_output_alias_never_writes_source_storage(self):
        with self.assertRaises(drill.UnsafeOutput):
            self.execute('output-alias')
        self.assertFalse((self.base / 'report.json').exists())
        self.assertFalse((self.base / 'work').exists())

    def test_missing_complete(self):
        (self.backup / 'LOCAL_COMPLETE').unlink()
        with self.assertRaisesRegex(IntegrityError, 'required backup artifact'):
            drill.validate_backup(self.backup)

    def test_malformed_manifest(self):
        for raw in ('{', '[]', 'null', '{}'):
            (self.backup / 'manifest.json').write_text(raw)
            with self.assertRaises(IntegrityError):
                drill.validate_backup(self.backup)

    def test_unsupported_format(self):
        self.manifest['formatVersion'] = 2
        self.reseal()
        with self.assertRaisesRegex(IntegrityError, 'formatVersion'):
            drill.validate_backup(self.backup)

    def test_artifact_checksum_failures(self):
        for filename in ('database.dump', 'media.tar', 'manifest.json'):
            with self.subTest(filename=filename):
                p = self.backup / filename
                saved = p.read_bytes()
                p.write_bytes(saved + b'corrupt')
                with self.assertRaises(IntegrityError):
                    drill.validate_backup(self.backup)
                p.write_bytes(saved)

    def test_checksum_path_and_duplicates(self):
        for line in ('0' * 64 + '  ../outside\n', '0' * 64 + '  /absolute\n',
                     (self.backup / 'SHA256SUMS').read_text() * 2):
            (self.backup / 'SHA256SUMS').write_text(line)
            with self.assertRaises(IntegrityError):
                drill.validate_backup(self.backup)

    def test_manifest_filename_and_hash_mismatch(self):
        self.manifest['media']['archiveFilename'] = '../media.tar'
        self.reseal()
        with self.assertRaisesRegex(IntegrityError, 'filenames'):
            drill.validate_backup(self.backup)
        self.manifest['media']['archiveFilename'] = 'media.tar'
        self.reseal()
        self.manifest['database']['dumpSha256'] = '0' * 64
        (self.backup / 'manifest.json').write_text(json.dumps(self.manifest))
        (self.backup / 'SHA256SUMS').write_text(''.join(drill.file_hash(self.backup / n) + '  ' + n + '\n'
                                                     for n in ('database.dump', 'media.tar', 'manifest.json')))
        with self.assertRaisesRegex(IntegrityError, 'Manifest checksum'):
            drill.validate_backup(self.backup)

    def test_symlink_artifact(self):
        path = self.backup / 'database.dump'
        saved = self.base / 'dump'
        path.rename(saved)
        path.symlink_to(saved)
        with self.assertRaises(IntegrityError):
            drill.validate_backup(self.backup)

    def test_malicious_and_truncated_archives(self):
        bad = [tarfile.TarInfo('/absolute'), tarfile.TarInfo('../escape'), tarfile.TarInfo('a/../../escape'),
               tarfile.TarInfo('a\\escape')]
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE, tarfile.FIFOTYPE):
            entry = tarfile.TarInfo('objects/link')
            entry.type, entry.linkname = kind, '../../outside'
            bad.append(entry)
        for member in bad:
            with self.subTest(name=member.name, type=member.type):
                (self.backup / 'media.tar').write_bytes(tar_bytes(entries=[member]))
                self.reseal()
                with self.assertRaises(IntegrityError):
                    drill.validate_backup(self.backup)
        (self.backup / 'media.tar').write_bytes(tar_bytes(self.files)[:800])
        self.reseal()
        with self.assertRaises(IntegrityError):
            drill.validate_backup(self.backup)

    def test_duplicate_and_file_ancestor_archive(self):
        for data in (tar_bytes(entries=[tarfile.TarInfo('same'), tarfile.TarInfo('./same')]),
                     tar_bytes({'a': b'x', 'a/b': b'y'})):
            (self.backup / 'media.tar').write_bytes(data)
            with self.assertRaises(IntegrityError):
                archive_inventory(self.backup / 'media.tar')

    def test_safe_publication_hardlink_is_harmless(self):
        link = tarfile.TarInfo('tmp/upload-still-linked')
        link.type, link.linkname = tarfile.LNKTYPE, './' + ORIGINAL_PATH
        (self.backup / 'media.tar').write_bytes(tar_bytes(self.files, entries=[link]))
        inv = archive_inventory(self.backup / 'media.tar')
        self.assertEqual(inv[link.name], inv[ORIGINAL_PATH])
        report, _ = verify_media(self.state_a, inv)
        self.assertEqual(report['extras']['tmpUploadLeftovers'], 2)

    def test_forward_cyclic_and_directory_hardlinks_refused(self):
        for target in ('not-yet-stored', 'link', '.', '/outside', '../outside'):
            link = tarfile.TarInfo('link')
            link.type, link.linkname = tarfile.LNKTYPE, target
            (self.backup / 'media.tar').write_bytes(tar_bytes(self.files, entries=[link]))
            with self.subTest(target=target), self.assertRaises(IntegrityError):
                archive_inventory(self.backup / 'media.tar')

    def test_target_guards(self):
        for field, value in [('environment', 'production'), ('environment', 'prod'), ('project', 'source'),
                             ('media_volume', 'source_media'), ('database_volume', 'source_database'),
                             ('database_name', 'source_db'), ('media_volume', 'drill-fixture_database'),
                             ('database_service', 'app'), ('project', 'drill-;touch'), ('disposable', False)]:
            a = copy.copy(self.args)
            setattr(a, field, value)
            with self.subTest(field=field, value=value), self.assertRaises(IntegrityError):
                drill.guard_targets(a, self.manifest)

    def test_original_failures(self):
        inv = archive_inventory(self.backup / 'media.tar')
        for replacement, message in [(None, 'Missing original'), ((999, inv[ORIGINAL_PATH][1]), 'size'),
                                     ((len(ORIGINAL), '0' * 64), 'digest')]:
            current = dict(inv)
            if replacement is None:
                del current[ORIGINAL_PATH]
            else:
                current[ORIGINAL_PATH] = replacement
            with self.subTest(message=message), self.assertRaisesRegex(IntegrityError, message):
                verify_media(self.state_a, current)

    def test_thumbnail_failures(self):
        inv = archive_inventory(self.backup / 'media.tar')
        for replacement in (None, (1, THUMB_SHA), (len(THUMB), '0' * 64)):
            current = dict(inv)
            if replacement is None:
                del current[THUMB_PATH]
            else:
                current[THUMB_PATH] = replacement
            with self.subTest(replacement=replacement), self.assertRaises(IntegrityError):
                verify_media(self.state_a, current)

    def test_harmless_leftovers_classified(self):
        state = copy.deepcopy(self.state_a)
        ident = 'med_' + 'b' * 32
        state['assets'].append({'id': ident, 'deleted': True})
        inv = archive_inventory(self.backup / 'media.tar')
        inv.update({f'objects/bb/{ident}': (1, 'x'), 'objects/cc/unknown': (1, 'x'),
                    f'thumbnails/aa/{ID}/stale': (1, 'x')})
        report, _ = verify_media(state, inv)
        self.assertEqual(report['extras'], dict(unreferencedOriginals=1, tombstonedLeftovers=1,
                                               staleThumbnails=1, tmpUploadLeftovers=1, other=0))

    def test_product_and_historical_reference_failure(self):
        for kind in ('product', 'historical', 'thumbnail', 'unsafe-legacy'):
            state = copy.deepcopy(self.state_a)
            if kind == 'product':
                state['productMedia'][0]['media_id'] = 'med_' + 'b' * 32
            elif kind == 'historical':
                state['historicalImages'] = ['/media/med_' + 'b' * 32]
            elif kind == 'thumbnail':
                state['historicalImages'] = ['/media/' + ID + '/thumbnail/' + '0' * 64]
            else:
                state['historicalImages'] = ['/assets/../secret']
            with self.subTest(kind=kind), self.assertRaises(IntegrityError):
                verify_media(state, archive_inventory(self.backup / 'media.tar'))

    def test_legacy_syntax(self):
        for ref in ('https://production.example/assets/x.png', '/assets/../x', '/assets/%2e%2e/x', '//assets/x', '/assets/x?query'):
            with self.subTest(ref=ref), self.assertRaises(IntegrityError):
                legacy_path(ref)

    def test_metadata_failures(self):
        for key, value in [('counts', {}), ('migrationMarker', []), ('postgresVersionNum', '150000'), ('extensions', ['plpgsql'])]:
            state = copy.deepcopy(self.state_a)
            state[key] = value
            with self.subTest(key=key), self.assertRaises(IntegrityError):
                drill.compare_metadata(self.manifest, state)

    def test_failure_injection_never_passes(self):
        for failure in ('pg_restore', 'counts', 'nonempty-db', 'nonempty-media', 'app-start', 'http', 'alias', 'existing', 'legacy',
                        'existing-publication', 'publication-participant', 'db-on-publication', 'network-alias', 'db-published'):
            with self.subTest(failure=failure):
                code, report, fake = self.execute(failure)
                self.assertEqual((code, report['verdict']), (1, 'FAIL'))
                if failure == 'pg_restore':
                    self.assertEqual(report['databaseRestore']['result'], 'FAIL')
                if failure in ('pg_restore', 'counts', 'nonempty-db', 'nonempty-media', 'alias', 'existing', 'legacy'):
                    self.assertFalse(any('up' in call and call[-1] == 'app' for call in fake.calls))
                (self.base / 'report.json').unlink()
                # Remove ONLY this test's own temporary fixture work files.
                import shutil
                shutil.rmtree(self.base / 'work', ignore_errors=True)

    def test_docker_denial_incomplete(self):
        code, report, fake = self.execute('denied')
        self.assertEqual((code, report['verdict']), (3, 'INCOMPLETE'))
        self.assertFalse((self.base / 'work').exists())

    def test_missing_sessions_incomplete(self):
        argv = self.argv[:-2]
        code, report, fake = self.execute(argv=argv)
        self.assertEqual((code, report['verdict']), (3, 'INCOMPLETE'))
        self.assertEqual(report['applicationReads']['admin'], 'INCOMPLETE')

    def test_output_never_overwrites_artifacts_or_existing_files(self):
        for output in (self.backup / 'restore-report.json', self.backup / 'manifest.json'):
            argv = list(self.argv)
            argv[argv.index('--report') + 1] = str(output)
            with self.assertRaises(IntegrityError):
                drill.main(argv)
        self.assertFalse((self.backup / 'restore-report.json').exists())

    def test_report_states(self):
        for checks, result in [({}, 'INCOMPLETE'), ({'a': 'PENDING'}, 'INCOMPLETE'),
                               ({'a': 'PASS'}, 'PASS'), ({'a': 'PASS', 'b': 'INCOMPLETE'}, 'INCOMPLETE'),
                               ({'a': 'FAIL', 'b': 'PENDING'}, 'FAIL')]:
            self.assertEqual(drill.verdict(checks), result)

    def test_release_override_never_proves_recovery(self):
        with patch.object(drill, 'run', return_value='2' * 40):
            # A different declared SHA is refused by guard_targets first.
            a = copy.copy(self.args)
            a.application_git_sha = '2' * 40
            with self.assertRaises(IntegrityError):
                drill.guard_targets(a, self.manifest)
            a.allow_release_mismatch = True
            drill.guard_targets(a, self.manifest)
        argv = self.argv + ['--allow-release-mismatch']
        argv[argv.index('--application-git-sha') + 1] = '2' * 40
        code, report, _ = self.execute(argv=argv)
        self.assertEqual((code, report['verdict']), (3, 'INCOMPLETE'))
        self.assertTrue(report['release']['override'])

    def test_archived_product_is_not_chosen_for_public_pdp(self):
        self.state_a['products'].insert(0, {'id': 'archived-first', 'archived': True, 'image': '', 'gallery': [], 'legacyRetired': True})
        (self.backup / 'database.dump').write_text(json.dumps(self.state_a))
        self.reseal()
        code, report, fake = self.execute()
        self.assertEqual((code, report['verdict']), (0, 'PASS'))
        self.assertIn('/api/products/product-a', fake.reads)
        self.assertNotIn('/api/products/archived-first', fake.reads)

    def test_resource_thresholds_leave_disk_headroom(self):
        class Usage:
            free = 1600 * 10**6
        with patch.object(drill.shutil, 'disk_usage', return_value=Usage()):
            with self.assertRaises(drill.Incomplete):
                drill.preflight_resources(256 * 1024**2)

    def test_resource_limitation_produces_incomplete(self):
        fake = FakeDocker(self)
        with patch.object(drill, 'run', fake.run), patch.object(drill, 'preflight_resources', side_effect=drill.Incomplete('Resource thresholds prevent a disposable restore')), patch('builtins.print'):
            self.assertEqual(drill.main(self.argv), 3)
        report = json.loads((self.base / 'report.json').read_text())
        self.assertEqual(report['verdict'], 'INCOMPLETE')
        self.assertFalse((self.base / 'work').exists())

    def test_missing_ack_is_cli_error(self):
        argv = [v for v in self.argv if v != '--disposable']
        with patch('sys.stderr', io.StringIO()), self.assertRaises(SystemExit) as err:
            drill.parse_args(argv)
        self.assertEqual(err.exception.code, 2)

    def test_current_checkout_changes_refused(self):
        report = {'checks': {}}
        with patch.object(drill, 'run', side_effect=[SHA, ' M README.md']):
            with self.assertRaisesRegex(IntegrityError, 'Checkout/release mismatch'):
                drill.drill(self.args, report)
        self.assertFalse((self.base / 'work').exists())

    def test_image_bytes_and_health_json_are_checked(self):
        class Response(io.BytesIO):
            status = 200
            headers = {'Content-Type': 'application/json'}
        opener = unittest.mock.Mock()
        for body, expected, path in [(b'{"status":"ok","storage":"memory"}', None, '/api/health'),
                                     (b'wrong-bytes', '0' * 64, '/media/' + ID)]:
            opener.open.return_value = Response(body)
            with patch.object(drill.urllib.request, 'build_opener', return_value=opener):
                with self.assertRaises(IntegrityError):
                    drill.read_check(38457, path, expected=expected)

    def test_no_redirect_or_proxy_or_remote_reads(self):
        self.assertIsNone(drill.NoRedirect().redirect_request(None, None, 302, '', {}, 'https://production.example'))
        with self.assertRaises(IntegrityError):
            drill.read_check(38457, '//production.example/path')
        with self.assertRaises(IntegrityError):
            drill.read_check(38457, '/path\r\nInjected: header')


if __name__ == '__main__':
    unittest.main()
