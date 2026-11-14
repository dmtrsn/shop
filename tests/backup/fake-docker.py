#!/usr/bin/python3
"""A disposable Docker CLI stand-in for orchestration tests."""
import json
import os
from pathlib import Path
import sys

base = Path(os.environ['BACKUP_TEST_DIR'])
args = sys.argv[1:]
with (base / 'calls').open('a') as f:
    f.write(json.dumps(args) + '\n')
state_file = base / 'state.json'
state = json.loads(state_file.read_text())
fail = os.environ.get('BACKUP_TEST_FAILURE', '')


def emit(obj):
    print(json.dumps(obj))


def container(name):
    if name == 'dbid':
        return {'Id': name, 'Image': 'dbimage', 'State': {'Running': True},
                'Config': {'Labels': {'com.docker.compose.project': 'test', 'com.docker.compose.service': 'postgres'}},
                'NetworkSettings': {'Networks': {'net': {'NetworkID': 'network-id', 'Aliases': ['postgres']}}},
                'Mounts': [{'Destination': '/var/lib/postgresql/data', 'Type': 'volume', 'Name': 'dbvol'}]}
    return {'Id': name, 'Image': 'appimage', 'State': {'Running': state['running'], 'ExitCode': 0, 'Health': {'Status': 'healthy'}},
            'Config': {'Labels': {'com.docker.compose.project': 'test', 'com.docker.compose.service': 'app'},
                       'Env': ['MEDIA_ROOT=' + ('/wrong' if fail == 'mount' else '/media'),
                               'DATABASE_URL=postgres://shop:private-secret@postgres:5432/shop?sslmode=disable']},
            'NetworkSettings': {'Networks': {'net': {'NetworkID': 'network-id', 'Aliases': ['app']}}},
            'Mounts': ([] if fail == 'mountabsent' else
                       [{'Destination': '/media', 'Type': 'volume', 'Name': 'mediavol', 'RW': True}] +
                       ([{'Destination': '/media/tmp', 'Type': 'volume', 'Name': 'other', 'RW': True}] if fail == 'nested' else []))}


if args[:2] == ['context', 'inspect']:
    emit([{'Endpoints': {'docker': {'Host': 'unix:///var/run/docker.sock'}}}])
elif args[:2] == ['compose', '--project-name']:
    if 'config' in args:
        emit({'services': {'app': {'environment': {'MEDIA_ROOT': '/media', 'DATABASE_URL': 'postgres://shop:private-secret@postgres:5432/shop?sslmode=disable'},
                                   'volumes': [{'type': 'volume', 'source': 'media_data', 'target': '/media'}]},
                           'postgres': {'volumes': [{'type': 'volume', 'source': 'postgres_data', 'target': '/var/lib/postgresql/data'}]}},
              'volumes': {'media_data': {'name': 'mediavol'}, 'postgres_data': {'name': 'dbvol'}}})
    elif args[-1] == '--quiet':
        print('appid\ndbid')
    else:
        print('appid' if args[-1] == 'app' else 'dbid')
elif args[:2] == ['container', 'inspect']:
    emit([container(args[2])])
elif args[:2] == ['volume', 'inspect']:
    name = args[2]
    emit([{'Driver': 'local', 'Mountpoint': str(base / name)}])
elif args[:2] == ['image', 'inspect']:
    emit([{'RepoDigests': ['image@sha256:' + 'a' * 64]}])
elif args[0] == 'exec':
    if 'pg_dump' in args:
        if '--version' in args:
            print('pg_dump (PostgreSQL) 16.4')
        elif fail == 'dump':
            sys.exit(1)
        else:
            sys.stdout.buffer.write(b'CUSTOM-DUMP')
    else:
        query = args[-1]
        if query == 'SHOW server_version_num':
            print('160004')
        elif query == 'SHOW server_version':
            print('16.4')
        elif query == 'SHOW data_directory':
            print('/var/lib/postgresql/data')
        elif query.startswith('SELECT pg_database_size'):
            print('1000')
        elif fail == 'metadata':
            sys.exit(1)
        else:
            emit({'migrationMarker': [{'version': '020', 'outcome': 'applied'}],
                  'representativeCounts': {'users': 2, 'cartItems': 3}})
elif args[0] == 'run':
    if 'du' in args:
        print(('999999999999' if fail == 'disk' else '1') + '\t/source')
    elif '--help' in args:
        print('tar help')
    elif fail == 'tar':
        sys.stdout.buffer.write(b'PARTIAL')
        sys.exit(1)
    else:
        sys.stdout.buffer.write(b'TAR-DATA')
elif args[0] == 'stop':
    if fail == 'stop':
        sys.exit(1)
    state['running'] = fail == 'refuse'
    state_file.write_text(json.dumps(state))
elif args[0] == 'start':
    with (base / 'calls').open('a') as f:
        f.write(json.dumps(['marker-at-start', bool(list((base / 'backups').rglob('LOCAL_COMPLETE')))]) + '\n')
    if fail == 'restart':
        sys.exit(1)
    state['running'] = True
    state_file.write_text(json.dumps(state))
else:
    sys.exit(2)
