#!/usr/bin/env python3
"""Restore an S1.1 set ONLY into a new, isolated, disposable Compose project."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import subprocess
import sys
import time
from urllib.parse import quote
import urllib.error
import urllib.request

from restore_drill_integrity import IntegrityError, archive_inventory, digest, require, verify_media

os.umask(0o077)
REPO = Path(__file__).resolve().parents[1]
COUNTS = {'users': 'users', 'products': 'products', 'orders': 'orders', 'orderItems': 'order_items',
          'cartItems': 'cart_items', 'mediaAssets': 'media_assets', 'productMedia': 'product_media',
          'mediaThumbnails': 'media_thumbnails'}
LABEL = 'shop.restore-drill'


class Incomplete(Exception):
    pass


class UnsafeOutput(IntegrityError):
    pass


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace('+00:00', 'Z')


def run(argv, *, stdin=None, stdout=None, timeout=180):
    # Suppress raw errors/output on failure: neither DSNs nor customer data may
    # escape into the report. No shell, eval, production URLs or inherited .env.
    try:
        result = subprocess.run(argv, stdin=stdin, stdout=stdout or subprocess.PIPE,
                                stderr=subprocess.DEVNULL, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise IntegrityError('Command unavailable or timed out: ' + argv[0]) from None
    require(result.returncode == 0, 'Command failed: ' + argv[0])
    return result.stdout.decode().strip() if stdout is None else None


def inspect(kind, ident):
    rows = json.loads(run(['docker', kind, 'inspect', ident]))
    require(len(rows) == 1, 'Ambiguous Docker resource')
    return rows[0]


def file_hash(path):
    with path.open('rb') as data:
        return digest(data)


def validate_backup(root):
    require(root.is_dir() and not root.is_symlink(), 'Backup directory is absent or a symlink')
    required = {'database.dump', 'media.tar', 'manifest.json', 'SHA256SUMS', 'LOCAL_COMPLETE'}
    for name in required:
        path = root / name
        require(path.is_file() and not path.is_symlink(), 'Missing or unsafe required backup artifact')
    try:
        m = json.loads((root / 'manifest.json').read_text())
        require(type(m['formatVersion']) is int and m['formatVersion'] == 1, 'Unsupported formatVersion')
        require(m['localCompletion']['status'] == 'complete', 'Backup local completion is absent')
        require(bool(re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', m['backupId'])), 'Invalid backup ID')
        require(bool(re.fullmatch(r'[0-9a-f]{40}', m['applicationGitSha'])), 'Invalid release SHA')
        require(isinstance(m['legacyAssetDependency'], str), 'Missing legacy release dependency')
        require(bool(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', m['media']['sourceIdentity'])),
                'Missing source media identity')
        require(bool(re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}', m['database']['name'])), 'Invalid source database identity')
        require(m['database']['dumpFilename'] == 'database.dump' and m['media']['archiveFilename'] == 'media.tar',
                'Unexpected artifact filenames')
        require(set(m['representativeCounts']) == set(COUNTS) and all(type(n) is int and n >= 0
                for n in m['representativeCounts'].values()), 'Invalid representative counts')
        require(isinstance(m['database']['migrationMarker'], list), 'Invalid migration marker')
        require(bool(re.match(r'^\d+\.', m['database']['postgresServerVersion'])), 'Invalid PostgreSQL version')
        require(isinstance(m['offHost']['status'], str) and m['offHost']['required'] is True, 'Missing off-host status/requirement')
        require(m['applicationImageDigest'] is None or bool(re.fullmatch(
            r'[A-Za-z0-9_./:-]+@sha256:[0-9a-f]{64}', m['applicationImageDigest'])), 'Invalid image digest')
        sums = {}
        for line in (root / 'SHA256SUMS').read_text().splitlines():
            match = re.fullmatch(r'([0-9a-f]{64})  (database.dump|media.tar|manifest.json)', line)
            require(match is not None, 'Unsafe checksum filename or syntax')
            sha, name = match.groups()
            require(name not in sums, 'Duplicate checksum filename')
            sums[name] = sha
        require(set(sums) == {'database.dump', 'media.tar', 'manifest.json'}, 'Incomplete checksums')
        for name, sha in sums.items():
            require(file_hash(root / name) == sha, 'Artifact checksum mismatch')
        for section, name, prefix in [('database', 'database.dump', 'dump'), ('media', 'media.tar', 'archive')]:
            require(m[section][prefix + 'Sha256'] == sums[name], 'Manifest checksum mismatch')
            require(type(m[section][prefix + 'Bytes']) is int and
                    m[section][prefix + 'Bytes'] == (root / name).stat().st_size, 'Manifest artifact size mismatch')
        inventory = archive_inventory(root / 'media.tar')
    except (ValueError, KeyError, TypeError, AttributeError):
        raise IntegrityError('Malformed S1.1 manifest/checksums') from None
    return m, inventory


def guard_targets(a, m):
    require(a.disposable, 'Explicit disposable acknowledgement required')
    for name in ('environment', 'project', 'database_service', 'database_name', 'database_user',
                 'media_volume', 'database_volume', 'source_project', 'source_database_volume'):
        require(bool(re.fullmatch(r'[a-z][a-z0-9_-]{0,62}', getattr(a, name))), 'Invalid target/source identifier')
    require(a.environment not in ('prod', 'production') and 'production' not in a.environment,
            'Production environments are refused')
    require(a.project.startswith('drill-') and a.project != a.source_project, 'Unsafe disposable project')
    require(a.database_service != 'app', 'Database service must differ from app')
    require(a.media_volume != a.database_volume and all(v.startswith(a.project + '_') for v in
            (a.media_volume, a.database_volume)), 'Target volumes must be distinct and scoped to project')
    sources = {m['media']['sourceIdentity'], a.source_database_volume}
    require(not sources.intersection({a.media_volume, a.database_volume}), 'Source and target volumes overlap')
    require(a.database_name != m['database']['name'], 'Disposable database must have its own explicit name')
    require(1024 <= a.port <= 65535, 'Use an unprivileged isolated port')
    require(bool(re.fullmatch(r'[0-9a-f]{40}', a.application_git_sha)), 'Invalid application release SHA')
    for image in (a.application_image, a.postgres_image):
        require(bool(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_./:@-]{0,255}', image)), 'Invalid image identifier')
    require(a.application_git_sha == m['applicationGitSha'] or a.allow_release_mismatch,
            'Declared application release differs from backup')


def compare_metadata(m, restored):
    require(restored['counts'] == m['representativeCounts'], 'Restored representative count mismatch')
    require(restored['migrationMarker'] == m['database']['migrationMarker'], 'Restored migration marker mismatch')
    require(int(restored['postgresVersionNum']) // 10000 == int(m['database']['postgresServerVersion'].split('.')[0]),
            'Incompatible PostgreSQL major version')
    require('pg_trgm' in restored['extensions'], 'Required pg_trgm extension is missing')


def verdict(checks):
    if 'FAIL' in checks.values():
        return 'FAIL'
    return 'PASS' if checks and all(x == 'PASS' for x in checks.values()) else 'INCOMPLETE'


def preflight_resources(required_bytes):
    available = int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines()
                         if line.startswith('MemAvailable:'))) * 1024
    state = {'availableRamBytes': available, 'homeFreeBytes': shutil.disk_usage('/home').free,
             'rootFreeBytes': shutil.disk_usage('/').free, 'estimatedAdditionalBytes': required_bytes}
    # Conservative headroom on both filesystems, even when Docker's storage
    # location cannot yet be inspected. Never consume the operator's floor.
    if (available < 1.5 * 1024**3 or state['homeFreeBytes'] < 1.5 * 10**9 + required_bytes
            or state['rootFreeBytes'] < 2 * 10**9 + required_bytes):
        raise Incomplete('Resource thresholds prevent a disposable restore')
    return state


def snapshot_sql():
    count_sql = ','.join("'%s',(SELECT count(*) FROM %s)" % item for item in COUNTS.items())
    return """SELECT json_build_object(
      'counts',json_build_object(%s),
      'migrationMarker',(SELECT COALESCE(json_agg(json_build_object('version',version,'outcome',outcome) ORDER BY version),'[]') FROM schema_migrations),
      'postgresVersionNum',current_setting('server_version_num'),
      'extensions',(SELECT json_agg(extname ORDER BY extname) FROM pg_extension),
      'assets',(SELECT COALESCE(json_agg(json_build_object('id',id,'size_bytes',size_bytes,'sha256',sha256,'deleted',deleted_at IS NOT NULL) ORDER BY id),'[]') FROM media_assets),
      'thumbnails',(SELECT COALESCE(json_agg(json_build_object('media_id',media_id,'size_bytes',size_bytes,'sha256',sha256) ORDER BY media_id),'[]') FROM media_thumbnails),
      'productMedia',(SELECT COALESCE(json_agg(json_build_object('product_id',product_id,'media_id',media_id,'role',role,'position',position) ORDER BY product_id,role,position),'[]') FROM product_media),
      'products',(SELECT COALESCE(json_agg(json_build_object('id',id,'image',image,'gallery',gallery,'archived',archived_at IS NOT NULL,'legacyRetired',legacy_media_retired_at IS NOT NULL) ORDER BY id),'[]') FROM products),
      'historicalImages',(SELECT COALESCE(json_agg(image ORDER BY order_id,position),'[]') FROM order_items))""" % count_sql


def compose_model(a, token, app_id, pg_id, password):
    labels = {LABEL: token}
    app_env = {'APP_MODE': 'production', 'RUN_MIGRATIONS': 'false', 'EMAIL_TRANSPORT': '',
               'HTTP_ADDR': ':8080', 'MEDIA_ROOT': '/var/lib/sweetshop/media',
               'WEB_DIR': '/app/web', 'MIGRATIONS_DIR': '/app/migrations',
               'DATABASE_URL': f'postgres://{a.database_user}:{password}@{a.database_service}:5432/{a.database_name}'
                               '?sslmode=disable&options=-c%20default_transaction_read_only%3Don',
               'DB_MAX_CONNS': '2', 'DB_MIN_CONNS': '1', 'SESSION_COOKIE_SECURE': 'true',
               'TRUSTED_ORIGINS': 'https://shop.drill.invalid',
               'STAFF_TRUSTED_ORIGINS': 'https://admin.drill.invalid'}
    # Explicitly erase image-level email configuration; never import host env.
    for name in ('SMTP_HOST', 'SMTP_PORT', 'SMTP_TLS', 'SMTP_USERNAME', 'SMTP_PASSWORD', 'SMTP_FROM', 'PUBLIC_SHOP_ORIGIN'):
        app_env[name] = ''
    return {'services': {
        a.database_service: {'image': pg_id, 'pull_policy': 'never', 'labels': labels,
            'environment': {'POSTGRES_USER': a.database_user, 'POSTGRES_DB': a.database_name,
                            'POSTGRES_PASSWORD': password, 'PGDATA': '/var/lib/postgresql/data/pgdata'},
            'volumes': [{'type': 'volume', 'source': 'database', 'target': '/var/lib/postgresql/data'}],
            'networks': ['isolated']},
        'app': {'image': app_id, 'pull_policy': 'never', 'labels': labels, 'environment': app_env,
            'entrypoint': ['/app/sweetshop'], 'read_only': True, 'cap_drop': ['ALL'],
            'security_opt': ['no-new-privileges:true'], 'tmpfs': ['/tmp:size=16m'],
            'ports': [{'host_ip': '127.0.0.1', 'published': str(a.port), 'target': 8080}],
            'volumes': [{'type': 'volume', 'source': 'media', 'target': '/var/lib/sweetshop/media'}],
            'networks': ['isolated']}},
        'volumes': {'database': {'name': a.database_volume, 'driver': 'local', 'labels': labels},
                    'media': {'name': a.media_volume, 'driver': 'local', 'labels': labels}},
        'networks': {'isolated': {'name': a.project + '_isolated', 'internal': True, 'labels': labels}}}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def read_check(port, path, cookie='', expected=None):
    require(path.startswith('/') and not path.startswith('//') and not any(c in path for c in '\r\n'),
            'Unsafe application read path')
    # No proxy inheritance, redirects, production hosts, response bodies or
    # credentials in diagnostics. Only literal local GETs against the drill.
    request = urllib.request.Request(f'http://127.0.0.1:{port}' + path,
                                     headers={'Cookie': cookie} if cookie else {})
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect()).open(request, timeout=5) as response:
            require(response.status == 200, 'Application read check failed')
            if expected:
                require(digest(response) == expected, 'Application image digest mismatch')
            else:
                require('json' in response.headers.get('Content-Type', ''), 'Expected application JSON response')
                body = json.load(response)
                require(isinstance(body, (dict, list)), 'Invalid application JSON response')
                if path == '/api/health':
                    require(isinstance(body, dict) and body.get('status') == 'ok' and body.get('storage') == 'postgres',
                            'Application is not healthy against PostgreSQL')
    except (urllib.error.URLError, ValueError, OSError):
        raise IntegrityError('Application read check failed') from None


def drill(a, report):
    root = Path(a.backup_dir)
    m, backup_inventory = validate_backup(root)
    control_hashes = {name: file_hash(root / name) for name in ('manifest.json', 'SHA256SUMS', 'LOCAL_COMPLETE')}
    report.update(backupId=m['backupId'], sourceRelease={k: m[k] for k in
                  ('applicationGitSha', 'applicationImageDigest', 'legacyAssetDependency')},
                  offHost=m['offHost'], artifactChecksums='PASS')
    report['checks']['artifacts'] = 'PASS'
    guard_targets(a, m)
    report['checks']['targetGuards'] = 'PASS'
    sha = run(['git', '-C', str(REPO), 'rev-parse', 'HEAD'])
    dirty = bool(run(['git', '-C', str(REPO), 'status', '--porcelain', '--untracked-files=no']))
    release_matches = sha == a.application_git_sha == m['applicationGitSha'] and not dirty
    require(release_matches or a.allow_release_mismatch, 'Checkout/release mismatch (including tracked changes)')
    required = 4 * (m['database']['dumpBytes'] + m['media']['archiveBytes']) + 256 * 1024**2
    report['resources'] = preflight_resources(required)
    require(shutil.disk_usage(Path(a.compose_file).parent.parent).free >= required, 'Insufficient drill filesystem space')
    try:
        contexts = json.loads(run(['docker', 'context', 'inspect']))
        endpoint = contexts[0]['Endpoints']['docker']['Host']
        if not os.environ.get('DOCKER_CONTEXT'):
            endpoint = os.environ.get('DOCKER_HOST') or endpoint
        require(endpoint.startswith('unix://'), 'Only a local Unix-socket daemon is supported')
        run(['docker', 'info', '--format', '{{.ServerVersion}}'])
    except IntegrityError:
        raise Incomplete('Local Docker capability check unavailable/denied') from None
    app = inspect('image', a.application_image)
    pg = inspect('image', a.postgres_image)
    image_matches = (m['applicationImageDigest'] in (app.get('RepoDigests') or []) if m['applicationImageDigest']
                     else (app['Config'].get('Labels') or {}).get('org.opencontainers.image.revision') == m['applicationGitSha'])
    require(image_matches or a.allow_release_mismatch, 'Application image release identity is unproven')
    report['release'] = {'checkoutSha': sha, 'trackedChanges': dirty, 'imageId': app['Id'],
                         'override': a.allow_release_mismatch, 'matches': release_matches and image_matches}
    report['checks']['matchingRelease'] = 'PASS' if release_matches and image_matches else 'INCOMPLETE'
    # Freeze image IDs. Compose cannot pull/build or consume the repository .env.
    sources = [inspect('volume', name) for name in (a.source_database_volume, m['media']['sourceIdentity'])]
    for source in sources:
        for output in (Path(a.compose_file).parent, Path(a.report)):
            source_path = Path(source['Mountpoint']).resolve()
            output_path = output.resolve()
            if output_path.is_relative_to(source_path) or source_path.is_relative_to(output_path):
                raise UnsafeOutput('Output overlaps source storage')
        require(source['Driver'] == 'local' and not source.get('Options'), 'Source volume alias/driver is unsupported')
        require(source.get('Labels', {}).get('com.docker.compose.project') == a.source_project,
                'Source volume/project inventory does not match Docker identity')
    require(sources[0]['Name'] != sources[1]['Name'], 'Source database/media identities are ambiguous')
    existing_volumes = run(['docker', 'volume', 'ls', '--format', '{{.Name}}']).splitlines()
    existing_networks = run(['docker', 'network', 'ls', '--format', '{{.Name}}']).splitlines()
    require(not set(existing_volumes).intersection({a.database_volume, a.media_volume}), 'Nonempty/existing target volume refused')
    require(a.project + '_isolated' not in existing_networks, 'Existing target network refused')
    require(not run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + a.project]),
            'Existing target Compose project refused')
    for cid in run(['docker', 'ps', '-aq']).splitlines():
        container = inspect('container', cid)
        for binding in (container.get('HostConfig', {}).get('PortBindings') or {}).values():
            require(all(int(b['HostPort'] or 0) != a.port for b in (binding or [])), 'Target port conflicts with an existing service')
    with socket.socket() as probe:
        try:
            probe.bind(('127.0.0.1', a.port))
        except OSError:
            raise IntegrityError('Target port is already bound') from None
    compose_file = Path(a.compose_file)
    work = compose_file.parent
    require(not work.exists(), 'Drill work directory must be new')
    work.mkdir(mode=0o700)
    (work / 'empty.env').write_text('')
    token = secrets.token_hex(16)
    model = compose_model(a, token, app['Id'], pg['Id'], secrets.token_hex(24))
    compose_file.write_text(json.dumps(model, indent=2) + '\n')
    compose = ['docker', 'compose', '--env-file', str(work / 'empty.env'), '--project-name', a.project,
               '--file', str(compose_file)]
    # Eliminate overrides/profiles and environment file hooks inherited from the
    # operator shell. Every service environment value is explicit in the model.
    for key in list(os.environ):
        if key.startswith('COMPOSE_'):
            del os.environ[key]
    run(compose + ['config', '--quiet'])
    uid = run(['docker', 'run', '--rm', '--network', 'none', '--read-only', '--entrypoint', 'id', app['Id'], '-u'])
    gid = run(['docker', 'run', '--rm', '--network', 'none', '--read-only', '--entrypoint', 'id', app['Id'], '-g'])
    require(uid.isdecimal() and gid.isdecimal() and int(uid) > 0, 'Application image must run as non-root')
    run(compose + ['up', '-d', '--no-deps', '--pull', 'never', a.database_service])
    db_id = run(compose + ['ps', '-q', a.database_service])
    require(bool(db_id) and '\n' not in db_id, 'Ambiguous target DB container')

    def prove_container(cid, service, volume, destination):
        c = inspect('container', cid)
        labels = c['Config'].get('Labels') or {}
        require(labels.get(LABEL) == token and labels.get('com.docker.compose.project') == a.project
                and labels.get('com.docker.compose.service') == service, 'Target container identity mismatch')
        mounts = [x for x in c['Mounts'] if x['Destination'] == destination]
        require(len(c['Mounts']) == 1 and len(mounts) == 1 and mounts[0]['Type'] == 'volume' and mounts[0]['Name'] == volume,
                'Target container has unsafe mount identity')
        require(not any(x.get('Name') in {s['Name'] for s in sources} or
                        x.get('Source') in {s['Mountpoint'] for s in sources} for x in c['Mounts']),
                'Source mount alias on target')
        net = inspect('network', a.project + '_isolated')
        require(net.get('Internal') is True and net.get('Labels', {}).get(LABEL) == token,
                'Target network is not isolated')
        require(set(c['NetworkSettings']['Networks']) == {net['Name']}, 'Unexpected target network')
        require(c['NetworkSettings']['Networks'][net['Name']]['NetworkID'] == net['Id'], 'Network identity alias')
        permitted = {db_id, cid}
        require(set(net.get('Containers', {})).issubset(permitted), 'Unknown container joined the drill network')
        return c, net

    db, network = prove_container(db_id, a.database_service, a.database_volume, '/var/lib/postgresql/data')
    targets = []
    for name in (a.database_volume, a.media_volume):
        # Compose may defer creating the media volume until app creation. Create
        # it explicitly with the invocation's label only if still absent.
        if name == a.media_volume and name not in run(['docker', 'volume', 'ls', '--format', '{{.Name}}']).splitlines():
            run(['docker', 'volume', 'create', '--driver', 'local', '--label', LABEL + '=' + token, name])
        volume = inspect('volume', name)
        require(volume.get('Labels', {}).get(LABEL) == token and volume['Driver'] == 'local' and not volume.get('Options'),
                'Target volume is not owned by this drill')
        require(not any(Path(volume['Mountpoint']).resolve() == Path(s['Mountpoint']).resolve() for s in sources),
                'Source/target mountpoint alias')
        targets.append(volume)
    report['isolation'] = {'source': {'project': a.source_project, 'databaseName': m['database']['name'],
         'databaseVolume': sources[0]['Name'], 'mediaVolume': sources[1]['Name'],
         'mountpoints': [s['Mountpoint'] for s in sources], 'identityOrigin': 'manifest media; explicit DB inventory; Docker volume project labels verified'},
         'target': {'project': a.project, 'dbContainerId': db_id, 'databaseVolume': a.database_volume,
         'mediaVolume': a.media_volume, 'mountpoints': [v['Mountpoint'] for v in targets],
         'networkId': network['Id'], 'network': network['Name'], 'port': a.port, 'bindAddress': '127.0.0.1'},
         'drillId': token, 'beforeRestore': 'PASS', 'afterStart': 'PENDING'}
    report['checks']['isolation'] = 'PENDING'
    pg_exec = ['docker', 'exec', db_id]

    def sql(query):
        return run(pg_exec + ['psql', '-X', '-A', '-t', '-h', '/var/run/postgresql', '-v', 'ON_ERROR_STOP=1',
                             '-U', a.database_user, '-d', a.database_name, '-c', query])

    for attempt in range(60):
        try:
            sql('SELECT 1')
            break
        except IntegrityError:
            if attempt == 59:
                raise
            time.sleep(1)
    require(sql("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','S','f')") == '0',
            'Nonempty target database refused')
    version = sql('SHOW server_version_num')
    require(int(version) // 10000 == int(m['database']['postgresServerVersion'].split('.')[0]), 'PostgreSQL major mismatch')
    report['databaseRestore'] = {'startedAtUtc': now(), 'postgresVersionNum': version, 'result': 'PENDING',
                                'ownership': 'no-owner/no-privileges; fresh disposable role', 'migrations': 'verify-only'}
    # Rehash immediately before consumption; backup is never opened for writing.
    require(file_hash(root / 'database.dump') == m['database']['dumpSha256'], 'Dump changed after validation')
    with (root / 'database.dump').open('rb') as data:
        run(['docker', 'exec', '-i', db_id, 'pg_restore', '--exit-on-error', '--single-transaction',
             '--no-owner', '--no-privileges', '-h', '/var/run/postgresql', '-U', a.database_user,
             '-d', a.database_name], stdin=data, timeout=1800)
    report['databaseRestore'].update(endedAtUtc=now(), result='PASS')
    restored = json.loads(sql('BEGIN READ ONLY; ' + snapshot_sql() + '; COMMIT').removeprefix('BEGIN\n').removesuffix('\nCOMMIT'))
    compare_metadata(m, restored)
    report['countComparison'] = {'expected': m['representativeCounts'], 'actual': restored['counts'], 'status': 'PASS'}
    report['checks']['database'] = 'PASS'
    # Verify metadata against the backup bytes before extraction, then against
    # an independent export of the actual target volume after extraction.
    verify_media(restored, backup_inventory)
    require(file_hash(root / 'media.tar') == m['media']['archiveSha256'], 'Media changed after validation')
    mount = 'type=volume,src=' + a.media_volume + ',dst=/restore'
    helper = ['docker', 'run', '--rm', '--network', 'none', '--read-only', '--mount', mount,
              '--entrypoint', '/bin/sh', pg['Id']]
    empty = 'test -z "$(find /restore -mindepth 1 -print -quit)"'
    run(helper + ['-c', empty])
    with (root / 'media.tar').open('rb') as data:
        run(['docker', 'run', '--rm', '-i', '--network', 'none', '--read-only', '--mount', mount,
             '--entrypoint', '/bin/sh', pg['Id'], '-c', empty + ' && tar --no-same-owner --no-same-permissions -C /restore -xf -'
             ' && chown -R ' + uid + ':' + gid + ' /restore'
             ' && find /restore -type d -exec chmod 750 {} +'
             ' && find /restore -type f -exec chmod 640 {} +'], stdin=data, timeout=1800)
    export = work / 'restored-media.tar'
    with export.open('xb') as out:
        run(['docker', 'run', '--rm', '--network', 'none', '--read-only', '--mount', mount + ',readonly',
             '--entrypoint', 'tar', pg['Id'], '-C', '/restore', '-cf', '-', '.'], stdout=out, timeout=1800)
    actual_inventory = archive_inventory(export)
    require(actual_inventory == backup_inventory, 'Restored media bytes differ from matched backup')
    integrity, legacy = verify_media(restored, actual_inventory)
    report['mediaIntegrity'] = integrity
    report['checks']['media'] = 'PASS'
    # Only paths derived from validated /assets references reach the image. No
    # arbitrary host copies or mounts are permitted to satisfy legacy checks.
    base = ['docker', 'run', '--rm', '--network', 'none', '--read-only', '--entrypoint', '/bin/sh', app['Id'], '-c']
    run(base + ['test -d /app/web/assets'])
    for start in range(0, len(legacy), 100):
        run(base + ['for asset do test -f "$asset" && test -r "$asset" || exit 1; done', 'legacy-check'] + legacy[start:start+100])
    integrity['legacyAssets'].update(status='PASS' if image_matches else 'INCOMPLETE',
                                     fileExistence='PASS', matchingImageRelease=image_matches)
    report['checks']['references'] = integrity['legacyAssets']['status']
    # Nothing starts until every DB/media/reference check above succeeds.
    run(compose + ['up', '-d', '--no-deps', '--pull', 'never', 'app'])
    app_id = run(compose + ['ps', '-q', 'app'])
    running, _ = prove_container(app_id, 'app', a.media_volume, '/var/lib/sweetshop/media')
    env = dict(item.split('=', 1) for item in running['Config']['Env'] if '=' in item)
    require(all(env.get(k) == v for k, v in model['services']['app']['environment'].items()), 'Restored app dependency configuration differs')
    require(running['Image'] == app['Id'], 'Unexpected application image')
    bindings = running['NetworkSettings']['Ports'].get('8080/tcp') or []
    require(bindings == [{'HostIp': '127.0.0.1', 'HostPort': str(a.port)}], 'Unexpected bound application port')
    report['isolation']['target']['appContainerId'] = app_id
    report['isolation']['afterStart'] = 'PASS'
    report['checks']['isolation'] = 'PASS'
    for attempt in range(60):
        try:
            read_check(a.port, '/api/health')
            break
        except IntegrityError:
            if attempt == 59 or not inspect('container', app_id)['State']['Running']:
                raise IntegrityError('Restored application failed to start') from None
            time.sleep(1)
    reads = {'health': 'PASS'}
    read_check(a.port, '/api/catalog')
    reads['catalog'] = 'PASS'
    active_products = [p for p in restored['products'] if not p['archived']]
    if active_products:
        read_check(a.port, '/api/products/' + quote(active_products[0]['id'], safe=''))
        reads['productDetail'] = 'PASS'
    else:
        reads['productDetail'] = 'INCOMPLETE'
    # Use valid restored fixture cookies supplied in a private file. No login or
    # fixture writes are performed. 401/403 never count as successful reads.
    sessions = {}
    if a.read_sessions_file:
        session_path = Path(a.read_sessions_file)
        st = session_path.stat()
        require(session_path.is_file() and not session_path.is_symlink() and st.st_uid == os.geteuid()
                and not st.st_mode & 0o077, 'Read session file must be private and owned by the operator')
        sessions = json.loads(session_path.read_text())
        require(isinstance(sessions, dict) and set(sessions).issubset({'staffCookie', 'customerCookie'})
                and all(isinstance(v, str) and '\r' not in v and '\n' not in v for v in sessions.values()),
                'Invalid read session file')
    for key, path, session_key in [('admin', '/api/admin/v1/products', 'staffCookie'),
                                   ('cart', '/api/cart', 'customerCookie'), ('orders', '/api/orders', 'customerCookie')]:
        if sessions.get(session_key):
            read_check(a.port, path, sessions[session_key])
            reads[key] = 'PASS'
        else:
            reads[key] = 'INCOMPLETE'
    product_assets = {row['media_id'] for row in restored['productMedia']
                      if row['product_id'] in {p['id'] for p in active_products}}
    originals = [name for name in actual_inventory if name.startswith('objects/') and name.split('/')[-1] in product_assets]
    if originals:
        name = originals[0]
        read_check(a.port, '/media/' + name.split('/')[-1], expected=actual_inventory[name][1])
        reads['productImage'] = 'PASS'
    elif any(p['image'].startswith('/assets/') and not p['legacyRetired'] for p in active_products):
        read_check_image = next(p['image'] for p in active_products if p['image'].startswith('/assets/') and not p['legacyRetired'])
        # A legacy file has no digest in S1.1; compare with the same release image.
        reads['productImage'] = legacy_http_check(a.port, read_check_image)
    else:
        reads['productImage'] = 'INCOMPLETE'
    historical = [ref for ref in restored['historicalImages'] if ref]
    if historical:
        ref = historical[0]
        if ref.startswith('/media/'):
            parts = ref.split('/')
            name = (f'thumbnails/{parts[2][4:6]}/{parts[2]}/{parts[4]}' if len(parts) == 5
                    else f'objects/{parts[2][4:6]}/{parts[2]}')
            read_check(a.port, ref, expected=actual_inventory[name][1])
            reads['historicalImage'] = 'PASS'
        else:
            reads['historicalImage'] = legacy_http_check(a.port, ref)
    else:
        reads['historicalImage'] = 'PASS'  # no historical images in this backup
    report['applicationReads'] = reads
    report['checks']['application'] = verdict(reads)
    after = json.loads(sql('BEGIN READ ONLY; ' + snapshot_sql() + '; COMMIT').removeprefix('BEGIN\n').removesuffix('\nCOMMIT'))
    require(after == restored, 'Restored DB/media image metadata changed during application verification')
    report['historicalSnapshots'] = {'status': 'PASS', 'comparison': 'all ordered image snapshots before/after app checks',
        'backupBoundary': 'validated pg_restore input; S1.1 has no separate source snapshot digest'}
    report['checks']['historicalUnchanged'] = 'PASS'
    final_manifest, final_inventory = validate_backup(root)
    require(final_manifest == m and final_inventory == backup_inventory and
            control_hashes == {name: file_hash(root / name) for name in control_hashes}, 'Source backup artifacts changed during drill')
    report['checks']['sourceArtifactsUnchanged'] = 'PASS'


def legacy_http_check(port, ref):
    # References were already checked by verify_media; redirects remain refused.
    require(ref.startswith(('/assets/', '/media/')) and not any(c in ref for c in '\r\n'), 'Unsafe image read path')
    try:
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        with opener.open(f'http://127.0.0.1:{port}' + ref, timeout=5) as response:
            require(response.status == 200 and response.headers.get('Content-Type', '').startswith('image/'),
                    'Historical/legacy image retrieval failed')
            require(bool(response.read(1)), 'Empty image response')
    except (urllib.error.URLError, OSError):
        raise IntegrityError('Historical/legacy image retrieval failed') from None
    return 'PASS'


def parse_args(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('backup-dir', 'environment', 'compose-file', 'project', 'database-service', 'database-name',
                 'database-user', 'database-volume', 'media-volume', 'source-project', 'source-database-volume',
                 'application-image', 'application-git-sha', 'postgres-image', 'report'):
        p.add_argument('--' + name, required=True)
    p.add_argument('--port', type=int, required=True)
    p.add_argument('--disposable', action='store_true', required=True)
    p.add_argument('--allow-release-mismatch', action='store_true', help='controlled tests only; recovery verdict stays INCOMPLETE')
    p.add_argument('--read-sessions-file', help='private JSON with customerCookie/staffCookie for already restored fixture sessions')
    return p.parse_args(argv)


def main(argv=None):
    a = parse_args(argv)
    # Resolve output aliases before opening anything writable. Backup artifacts
    # and pre-existing work/report files are never overwritten.
    root = Path(a.backup_dir).resolve(strict=True)
    report_path = Path(a.report)
    compose_path = Path(a.compose_file)
    for path in (report_path, compose_path):
        require(path.is_absolute() and not path.exists() and not path.is_symlink(), 'Output must be a new absolute path')
        require(not path.resolve().is_relative_to(root) and not root.is_relative_to(path.resolve()), 'Output overlaps backup')
    require(not report_path.resolve().is_relative_to(compose_path.parent.resolve()), 'Report must be outside the new Compose work directory')
    require(report_path.parent.is_dir(), 'Report parent must already exist')
    started = time.monotonic()
    report = {'formatVersion': 1, 'startedAtUtc': now(), 'targetProject': a.project,
              'cleanup': 'KEEP: no automatic removal of any resources',
              'artifactChecksums': 'PENDING', 'databaseRestore': {'result': 'PENDING'},
              'countComparison': {'status': 'PENDING'}, 'mediaIntegrity': {'status': 'PENDING'},
              'historicalSnapshots': {'status': 'PENDING'}, 'applicationReads': {}, 'isolation': {'status': 'PENDING'},
              'checks': {k: 'PENDING' for k in ('artifacts', 'targetGuards', 'matchingRelease', 'database', 'media',
                        'references', 'isolation', 'application', 'historicalUnchanged', 'sourceArtifactsUnchanged')}}
    try:
        drill(a, report)
    except UnsafeOutput:
        raise
    except Incomplete as exc:
        report['limitation'] = str(exc)
    except IntegrityError as exc:
        report['failure'] = str(exc)
        report['checks']['execution'] = 'FAIL'
        if report['databaseRestore'].get('startedAtUtc') and report['databaseRestore']['result'] == 'PENDING':
            report['databaseRestore'].update(result='FAIL', endedAtUtc=now())
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        report['failure'] = 'Invalid configuration, command response, or filesystem operation'
        report['checks']['execution'] = 'FAIL'
    except (KeyboardInterrupt, SystemExit):
        report['limitation'] = 'Drill interrupted; resources kept'
    report.update(endedAtUtc=now(), durationSeconds=round(time.monotonic() - started, 3), verdict=verdict(report['checks']))
    # Exclusive creation, private permissions, fixed report fields; no SQL rows,
    # cookies, passwords, command stderr or HTTP response bodies are serialized.
    with report_path.open('x') as out:
        json.dump(report, out, indent=2)
        out.write('\n')
    print('DISPOSABLE RESTORE: ' + report['verdict'] + '; report: ' + str(report_path))
    return {'PASS': 0, 'FAIL': 1, 'INCOMPLETE': 3}[report['verdict']]


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (IntegrityError, OSError, ValueError):
        print('Restore drill refused unsafe/invalid arguments or report destination', file=sys.stderr)
        sys.exit(2)
