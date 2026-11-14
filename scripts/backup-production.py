#!/usr/bin/env python3
"""Create a local, quiesced Compose backup. See README for operator duties."""
import argparse
import ctypes
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import subprocess
import sys
import time
from urllib.parse import parse_qsl, unquote, urlsplit

os.umask(0o077)


class BackupError(Exception):
    pass


def fail(message):
    raise BackupError(message)


def run(args, *, output=None):
    # Never forward command stderr: Compose or PostgreSQL may include credentials.
    try:
        result = subprocess.run(args, stdout=output or subprocess.PIPE,
                                stderr=subprocess.DEVNULL, check=False)
    except OSError:
        fail('Cannot execute ' + args[0])
    if result.returncode:
        fail('Command failed: ' + ' '.join(args[:2]))
    return result.stdout.decode().strip() if output is None else None


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def inspect(kind, ident):
    data = json.loads(run(['docker', kind, 'inspect', ident]))
    if len(data) != 1:
        fail('Ambiguous Docker identity')
    return data[0]


def envmap(container):
    return dict(x.split('=', 1) for x in container['Config']['Env'] if '=' in x)


def hash_file(path):
    digest = hashlib.sha256()
    with path.open('rb') as src:
        for block in iter(lambda: src.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def inside(a, b):
    return a == b or b in a.parents


def publish(source, target):
    # Both names are in the same directory; never replace an existing set.
    libc = ctypes.CDLL(None, use_errno=True)
    renameat2 = getattr(libc, 'renameat2', None)
    if renameat2 is None:
        fail('Atomic no-replace directory rename is unavailable')
    renameat2.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    if renameat2(-100, os.fsencode(source), -100, os.fsencode(target), 1) != 0:
        fail('Atomic backup publication failed')


def parse_args():
    p = argparse.ArgumentParser(description=__doc__)
    for option in ('environment', 'compose-file', 'project', 'backup-root', 'database-service',
                   'database-name', 'database-user', 'writer-service', 'media-root', 'application-git-sha', 'lock-file'):
        p.add_argument('--' + option, required=True)
    p.add_argument('--ack-external-writers-stopped', action='store_true', required=True)
    args = p.parse_args()
    for name in ('environment', 'project', 'database_service', 'database_name', 'database_user', 'writer_service'):
        if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}', getattr(args, name)):
            p.error('Invalid ' + name.replace('_', '-'))
    if not re.fullmatch(r'[0-9a-fA-F]{40}', args.application_git_sha):
        p.error('application-git-sha must be a full 40-character Git SHA')
    if not os.path.isabs(args.media_root) or os.path.normpath(args.media_root) != args.media_root or args.media_root == '/':
        p.error('media-root must be a normalized absolute path')
    if not all(os.path.isabs(x) for x in (args.backup_root, args.compose_file, args.lock_file)):
        p.error('backup-root, compose-file and lock-file must be absolute paths')
    if args.writer_service != 'app' or args.database_service == args.writer_service:
        p.error('This topology supports the app writer and a separate database service')
    return args


def main():
    args = parse_args()
    for binary in ('docker',):
        if not shutil.which(binary):
            fail('Required command unavailable: ' + binary)
    # Local filesystem checks are meaningful only with a local Unix-socket daemon.
    if os.environ.get('DOCKER_HOST', '').startswith(('tcp:', 'ssh:')):
        fail('Remote Docker daemons are unsupported')
    contexts = json.loads(run(['docker', 'context', 'inspect']))
    endpoint = contexts[0]['Endpoints']['docker']['Host']
    if os.environ.get('DOCKER_HOST'):
        endpoint = os.environ['DOCKER_HOST']
    if not endpoint.startswith('unix://'):
        fail('A local Unix-socket Docker daemon is required')
    compose_file = Path(args.compose_file).resolve(strict=True)
    compose = ['docker', 'compose', '--project-name', args.project, '--file', str(compose_file)]
    config = json.loads(run(compose + ['config', '--format', 'json']))
    services = config['services']
    if args.writer_service not in services or args.database_service not in services:
        fail('Configured writer or database service is absent')
    for name, service in services.items():
        if name not in (args.writer_service, args.database_service) and 'DATABASE_URL' in (service.get('environment') or {}):
            fail('Additional configured database writer requires explicit inventory')

    def ids(service):
        return run(compose + ['ps', '--all', '--quiet', service]).splitlines()

    writer_ids, db_ids = ids(args.writer_service), ids(args.database_service)
    if not writer_ids or len(db_ids) != 1:
        fail('Expected existing writer containers and exactly one database container')
    db = inspect('container', db_ids[0])
    def check_labels(container, service):
        labels = container['Config'].get('Labels') or {}
        if (labels.get('com.docker.compose.project') != args.project or
                labels.get('com.docker.compose.service') != service or
                labels.get('com.docker.compose.oneoff', 'False').lower() == 'true'):
            fail('Container does not match the specified Compose project and service')
    check_labels(db, args.database_service)
    if not db['State']['Running']:
        fail('Database container is not running')
    db_volumes = [m for m in db['Mounts'] if m['Destination'] == '/var/lib/postgresql/data' and m['Type'] == 'volume']
    if len(db_volumes) != 1:
        fail('Database data volume is not uniquely identified')
    writers = [inspect('container', x) for x in writer_ids]
    media_names = set()
    image_ids = set()
    for writer in writers:
        check_labels(writer, args.writer_service)
        writer_networks = writer['NetworkSettings']['Networks']
        db_networks = db['NetworkSettings']['Networks']
        if not any(name in db_networks and args.database_service in (db_networks[name].get('Aliases') or [])
                   and (not net.get('NetworkID') or net.get('NetworkID') == db_networks[name].get('NetworkID'))
                   for name, net in writer_networks.items()):
            fail('Writer and database do not share the expected Compose network alias')
        env = envmap(writer)
        if env.get('MEDIA_ROOT') != args.media_root:
            fail('Effective MEDIA_ROOT does not match the requested mount')
        if env.get('DATABASE_URL') != (services[args.writer_service].get('environment') or {}).get('DATABASE_URL'):
            fail('Running writer database configuration differs from Compose')
        mounts = [m for m in writer['Mounts'] if m['Destination'] == args.media_root and m['Type'] == 'volume' and m['RW']]
        if len(mounts) != 1:
            fail('Managed media volume is absent or ambiguous')
        if any(m['Destination'].startswith(args.media_root + '/') for m in writer['Mounts']):
            fail('Nested media mounts cannot be captured by a single volume archive')
        media_names.add(mounts[0]['Name'])
        image_ids.add(writer['Image'])
        # Require the app's configured DB endpoint to be the explicit Compose DB.
        try:
            url = urlsplit(env['DATABASE_URL'])
            query = parse_qsl(url.query, keep_blank_values=True, strict_parsing=True)
            match = (url.scheme in ('postgres', 'postgresql') and url.hostname == args.database_service
                     and (url.port or 5432) == 5432 and unquote(url.path.lstrip('/')) == args.database_name
                     and unquote(url.username or '') == args.database_user
                     and (not query or (len(query) == 1 and query[0][0] == 'sslmode'
                                        and query[0][1] in ('disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full'))))
        except (KeyError, ValueError):
            match = False
        if not match:
            fail('App database identity does not match explicit Compose database')
    if len(media_names) != 1 or len(image_ids) != 1:
        fail('Writer replicas have different media or image identities')
    media_name = media_names.pop()
    if media_name == db_volumes[0]['Name']:
        fail('Media and database volumes must be distinct')
    media_volume = inspect('volume', media_name)
    db_volume = inspect('volume', db_volumes[0]['Name'])
    if any(v['Driver'] != 'local' or v.get('Options') for v in (media_volume, db_volume)):
        fail('Only plain local Docker volumes without driver options are supported')
    # The current Compose model must name these exact persistent volumes.
    def configured_volume(service, destination):
        matches = [v for v in service.get('volumes', [])
                   if v.get('type') == 'volume' and v.get('target') == destination]
        if len(matches) != 1:
            fail('Compose persistent volume is missing or ambiguous')
        source = matches[0]['source']
        return config['volumes'][source]['name']
    if (configured_volume(services[args.writer_service], args.media_root) != media_name or
            configured_volume(services[args.database_service], '/var/lib/postgresql/data') != db_volumes[0]['Name'] or
            (services[args.writer_service].get('environment') or {}).get('MEDIA_ROOT') != args.media_root):
        fail('Running containers do not match the current Compose configuration')
    for cid in run(compose + ['ps', '--all', '--quiet']).splitlines():
        if cid in writer_ids or cid in db_ids:
            continue
        other = inspect('container', cid)
        if other['State']['Running'] and any(m.get('Type') == 'volume' and m.get('Name') == media_name and m.get('RW') for m in other['Mounts']):
            fail('Another Compose container has writable access to managed media')
    media_path = Path(media_volume['Mountpoint']).resolve()
    db_path = Path(db_volume['Mountpoint']).resolve()
    root = Path(args.backup_root).resolve()
    lock_path = Path(args.lock_file)
    lock_parent = lock_path.parent.resolve(strict=True)
    if root == Path('/') or any(inside(root, p) or inside(p, root) or inside(lock_parent, p) for p in (media_path, db_path)):
        fail('Unsafe backup destination overlaps persistent source')
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    st = root.stat()
    if not root.is_dir() or st.st_uid != os.geteuid() or st.st_mode & 0o022 or not os.access(root, os.W_OK | os.X_OK):
        fail('Backup destination must be writable, owned by the operator and not group/world writable')
    # All backups of this deployment must use the same explicitly chosen lock.
    lock_fd = os.open(lock_path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    lock = os.fdopen(lock_fd, 'a')
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        fail('Another backup holds the destination lock')

    pg = ['docker', 'exec', db_ids[0]]
    def sql(query):
        return run(pg + ['psql', '-X', '-A', '-t', '-h', '/var/run/postgresql', '-p', '5432', '-v', 'ON_ERROR_STOP=1', '-U', args.database_user,
                         '-d', args.database_name, '-c', query])
    server_num = int(sql('SHOW server_version_num'))
    server_version = sql('SHOW server_version')
    data_directory = sql('SHOW data_directory')
    if (os.path.normpath(data_directory) != data_directory or
            (not data_directory.startswith('/var/lib/postgresql/data/') and data_directory != '/var/lib/postgresql/data')):
        fail('PostgreSQL data directory is outside the configured persistent volume')
    client_version = run(pg + ['pg_dump', '--version'])
    version_match = re.search(r'\b(\d+)(?:\.\d+)?', client_version)
    if not version_match or int(version_match.group(1)) < server_num // 10000:
        fail('pg_dump client is older than the PostgreSQL server')
    db_bytes = int(sql('SELECT pg_database_size(current_database())'))
    image = db['Image']
    mount = 'type=volume,src=' + media_name + ',dst=/source,readonly'
    media_cmd = ['docker', 'run', '--rm', '--network', 'none', '--read-only', '--mount', mount,
                 '--entrypoint', 'du', image, '-sk', '/source']
    media_kib = int(run(media_cmd).split()[0])
    run(['docker', 'run', '--rm', '--network', 'none', '--read-only', '--entrypoint', 'tar', image, '--help'])
    image_info = inspect('image', next(iter(image_ids)))
    digests = image_info.get('RepoDigests') or []
    # Twice both source sizes, plus 256 MiB, then another 25% headroom.
    required = (2 * db_bytes + 2 * media_kib * 1024 + 256 * 1024 * 1024) * 5 // 4
    available = shutil.disk_usage(root).free
    print(f'Destination filesystem: {root} (device {root.stat().st_dev}); estimated required bytes: {required}; available bytes: {available}', flush=True)
    if available < required:
        fail('Insufficient backup filesystem space')

    # Re-inspect immediately before stopping; record exactly those running.
    running = [cid for cid in writer_ids if inspect('container', cid)['State']['Running']]
    stopped_attempted = False
    published = False
    final = None
    error = None
    def interrupted(signum, _frame):
        raise BackupError('Interrupted by signal ' + str(signum))
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    try:
        backup_id = dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + secrets.token_hex(6)
        partial = root / (backup_id + '.partial')
        final = root / backup_id
        partial.mkdir(mode=0o700)
        started = now()
        if running:
            stopped_attempted = True
            run(['docker', 'stop', '--time', '30'] + running)
            for cid in running:
                state = inspect('container', cid)['State']
                if state.get('ExitCode') != 0 or state.get('OOMKilled'):
                    fail('Writer did not exit cleanly during quiescence')
        def verify_stopped():
            if any(inspect('container', cid)['State']['Running'] for cid in writer_ids):
                fail('Writer container is still running')
        verify_stopped()
        metadata_sql = "SELECT json_build_object('migrationMarker',(SELECT COALESCE(json_agg(json_build_object('version',version,'outcome',outcome) ORDER BY version),'[]'::json) FROM schema_migrations),'representativeCounts',json_build_object('users',(SELECT count(*) FROM users),'products',(SELECT count(*) FROM products),'orders',(SELECT count(*) FROM orders),'orderItems',(SELECT count(*) FROM order_items),'cartItems',(SELECT count(*) FROM cart_items),'mediaAssets',(SELECT count(*) FROM media_assets),'productMedia',(SELECT count(*) FROM product_media),'mediaThumbnails',(SELECT count(*) FROM media_thumbnails)))"
        metadata = json.loads(sql(metadata_sql))
        with (partial / 'database.dump').open('xb') as out:
            run(pg + ['pg_dump', '-Fc', '-h', '/var/run/postgresql', '-p', '5432', '-U', args.database_user, '-d', args.database_name], output=out)
        verify_stopped()
        with (partial / 'media.tar').open('xb') as out:
            run(['docker', 'run', '--rm', '--network', 'none', '--read-only', '--mount', mount,
                 '--entrypoint', 'tar', image, '-C', '/source', '-cf', '-', '.'], output=out)
        verify_stopped()
        ended = now()
        dump, archive = partial / 'database.dump', partial / 'media.tar'
        dump_hash, media_hash = hash_file(dump), hash_file(archive)
        manifest = {'formatVersion': 1, 'backupId': backup_id, 'environment': args.environment,
                    'createdAtUtc': now(), 'quiesceStartedAtUtc': started, 'quiesceEndedAtUtc': ended,
                    'applicationGitSha': args.application_git_sha.lower(),
                    'applicationImageDigest': digests[0] if len(digests) == 1 else None,
                    'legacyAssetDependency': 'Retain the matching application release and web/assets; Git SHA is operator supplied.',
                    'database': {'name': args.database_name, 'postgresServerVersion': server_version,
                                 'dumpClientVersion': client_version, 'dumpFilename': dump.name,
                                 'dumpBytes': dump.stat().st_size, 'dumpSha256': dump_hash,
                                 'migrationMarker': metadata['migrationMarker']},
                    'media': {'effectiveRoot': args.media_root, 'sourceIdentity': media_name,
                              'archiveFilename': archive.name, 'archiveBytes': archive.stat().st_size,
                              'archiveSha256': media_hash},
                    'representativeCounts': metadata['representativeCounts'],
                    'localCompletion': {'status': 'complete', 'completedAtUtc': now()},
                    'offHost': {'status': 'not_verified', 'required': True}}
        manifest_file = partial / 'manifest.json'
        with manifest_file.open('x', encoding='utf-8') as out:
            json.dump(manifest, out, indent=2, ensure_ascii=True)
            out.write('\n')
        with (partial / 'SHA256SUMS').open('x', encoding='ascii') as out:
            for path in (dump, archive, manifest_file):
                out.write(f'{hash_file(path)}  {path.name}\n')
        publish(partial, final)
        # Marker is made visible only after the directory has been published.
        marker_temp = final / '.LOCAL_COMPLETE.tmp'
        with marker_temp.open('x', encoding='ascii') as out:
            out.write('LOCAL COMPLETE; off-host copy not verified\n')
        os.link(marker_temp, final / 'LOCAL_COMPLETE')
        published = True
        marker_temp.unlink()
    except (Exception, KeyboardInterrupt) as exc:
        error = exc
    finally:
        # An interruption during recovery must not skip the recovery attempt.
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        if stopped_attempted:
            try:
                restart_failed = False
                for cid in running:
                    try:
                        if not inspect('container', cid)['State']['Running']:
                            run(['docker', 'start', cid])
                    except Exception:
                        restart_failed = True
                if restart_failed:
                    fail('At least one writer could not be restarted')
                deadline = time.monotonic() + 180
                while True:
                    states = [inspect('container', cid)['State'] for cid in running]
                    if all(s['Running'] and ('Health' not in s or s['Health']['Status'] == 'healthy') for s in states):
                        break
                    if time.monotonic() >= deadline or any(not s['Running'] or s.get('Health', {}).get('Status') == 'unhealthy' for s in states):
                        fail('Writer service failed to recover or become healthy')
                    time.sleep(1)
            except Exception as exc:
                print('OPERATIONAL FAILURE: writer recovery failed' + ('; backup artifacts are LOCAL COMPLETE' if published else ''), file=sys.stderr)
                error = error or exc
    if error:
        raise error
    print(f'LOCAL COMPLETE: {final}; off-host status: not_verified (required)', flush=True)


if __name__ == '__main__':
    try:
        main()
    except (BackupError, OSError, ValueError, KeyError, json.JSONDecodeError) as exc:
        print('Backup failed: ' + str(exc), file=sys.stderr)
        sys.exit(1)
