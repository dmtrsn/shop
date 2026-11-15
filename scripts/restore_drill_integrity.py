"""Read-only integrity helpers for S1.1 archives and restored media exports.

No extraction, repair, database writes, or dependency on Docker. Inventories
contain relative media names and digests; callers must not publish DB snapshots.
"""
import hashlib
from pathlib import PurePosixPath
import re
import tarfile


class IntegrityError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise IntegrityError(message)


def digest(stream):
    h = hashlib.sha256()
    for block in iter(lambda: stream.read(1024 * 1024), b''):
        h.update(block)
    return h.hexdigest()


def archive_inventory(path):
    """Validate every entry and read every byte before any extraction is allowed.

    Symlinks are refused. An internal hardlink may name only an already checked
    file: a failed temporary unlink after media publication can leave one.
    Forward links, cycles, duplicate entries and special files are refused.
    """
    inventory, names = {}, set()
    try:
        with open(path, 'rb') as data:
            data.seek(0, 2)
            size = data.tell()
            require(size >= 1024 and size % 512 == 0, 'Truncated media archive')
            data.seek(-1024, 2)
            require(data.read() == bytes(1024), 'Missing archive end markers')
        with tarfile.open(path, 'r:') as archive:
            for entry in archive:
                raw = entry.name
                parts = PurePosixPath(raw).parts
                require(raw and not raw.startswith('/') and '\\' not in raw
                        and '..' not in parts and not any(ord(c) < 32 for c in raw),
                        'Unsafe archive path')
                name = str(PurePosixPath(raw))
                require(name not in names, 'Duplicate archive path')
                names.add(name)
                require(entry.isdir() or entry.isfile() or entry.islnk(), 'Archive symlinks/special entries are refused')
                require(not entry.mode & 0o7000 and not entry.sparse,
                        'Archive privileged modes/sparse entries are refused')
                if entry.islnk():
                    target = entry.linkname
                    require(target and not target.startswith('/') and '\\' not in target and
                            '..' not in PurePosixPath(target).parts and not any(ord(c) < 32 for c in target),
                            'Unsafe archive hardlink target')
                    require(entry.size == 0 and str(PurePosixPath(target)) in inventory,
                            'Archive hardlink must name a previous checked file')
                    inventory[name] = inventory[str(PurePosixPath(target))]
                elif entry.isfile():
                    require(name != '.', 'Invalid archive file')
                    with archive.extractfile(entry) as data:
                        inventory[name] = (entry.size, digest(data))
            for name in names:
                require(not any(str(parent) in inventory for parent in PurePosixPath(name).parents),
                        'Archive file is an ancestor of another entry')
    except (tarfile.TarError, EOFError, OSError):
        raise IntegrityError('Unreadable or truncated media archive') from None
    return inventory


def managed_path(ref, assets, thumbnails):
    match = re.fullmatch(r'/media/(med_[0-9a-f]{32})(?:/thumbnail/([0-9a-f]{64}))?', ref)
    require(match is not None, 'Invalid managed image reference')
    ident, sha = match.groups()
    require(ident in assets and not assets[ident]['deleted'], 'Image reference has no live managed asset')
    if sha:
        require(ident in thumbnails and thumbnails[ident]['sha256'] == sha,
                'Historical thumbnail reference has no matching recorded thumbnail')
        return f'thumbnails/{ident[4:6]}/{ident}/{sha}'
    return f'objects/{ident[4:6]}/{ident}'


def legacy_path(ref):
    require(bool(re.fullmatch(r'/assets/[A-Za-z0-9_./-]+', ref))
            and '..' not in PurePosixPath(ref).parts and '//' not in ref,
            'Unsafe or unsupported legacy image reference')
    return '/app/web' + ref


def verify_media(snapshot, inventory):
    """Prove every live original/recorded thumbnail, then all image references.

    Snapshot comes from a read-only transaction on the restored database. Extra
    bytes are classified, never removed. Reports contain counts only.
    """
    assets = {a['id']: a for a in snapshot['assets']}
    thumbnails = {t['media_id']: t for t in snapshot['thumbnails']}
    required = set()
    originals = thumbs = 0
    for ident, asset in assets.items():
        require(bool(re.fullmatch(r'med_[0-9a-f]{32}', ident)), 'Invalid media ID')
        if asset['deleted']:
            continue
        name = f'objects/{ident[4:6]}/{ident}'
        require(name in inventory, 'Missing original')
        require(inventory[name][0] == asset['size_bytes'], 'Original size mismatch')
        require(inventory[name][1] == asset['sha256'], 'Original digest mismatch')
        required.add(name)
        originals += 1
        if ident in thumbnails:
            row = thumbnails[ident]
            name = f'thumbnails/{ident[4:6]}/{ident}/{row["sha256"]}'
            require(name in inventory, 'Missing recorded thumbnail')
            require(inventory[name] == (row['size_bytes'], row['sha256']), 'Thumbnail size/digest mismatch')
            required.add(name)
            thumbs += 1
    associations = set()
    product_ids = {p['id'] for p in snapshot['products']}
    for row in snapshot['productMedia']:
        require(row['product_id'] in product_ids, 'Missing associated product')
        role, position = row['role'], row['position']
        require(role in ('primary', 'gallery') and position >= 0 and (role != 'primary' or position == 0),
                'Invalid primary/gallery association')
        key = (row['product_id'], role, position)
        require(key not in associations, 'Duplicate product association')
        associations.add(key)
        require(managed_path('/media/' + row['media_id'], assets, thumbnails) in required,
                'Product association lacks valid bytes')
    legacy, historical = set(), 0
    refs = list(snapshot['historicalImages'])
    historical = len(refs)
    for product in snapshot['products']:
        refs.extend([product['image']] + product['gallery'])
    for ref in refs:
        if not ref:
            continue
        if ref.startswith('/media/'):
            require(managed_path(ref, assets, thumbnails) in required, 'Image reference lacks valid bytes')
        else:
            legacy.add(legacy_path(ref))
    extras = dict(unreferencedOriginals=0, tombstonedLeftovers=0, staleThumbnails=0,
                  tmpUploadLeftovers=0, other=0)
    for name in inventory.keys() - required:
        parts = PurePosixPath(name).parts
        ident = parts[2] if len(parts) > 2 else ''
        if ident in assets and assets[ident]['deleted']:
            extras['tombstonedLeftovers'] += 1
        elif name.startswith('objects/'):
            extras['unreferencedOriginals'] += 1
        elif name.startswith('thumbnails/'):
            extras['staleThumbnails'] += 1
        elif name.startswith('tmp/'):
            extras['tmpUploadLeftovers'] += 1
        else:
            extras['other'] += 1
    return ({'originals': {'status': 'PASS', 'checked': originals},
             'thumbnails': {'status': 'PASS', 'checked': thumbs}, 'extras': extras,
             'productReferences': {'status': 'PASS', 'checked': len(associations)},
             'historicalImages': {'status': 'PASS', 'checked': historical},
             'legacyAssets': {'status': 'PENDING', 'checked': len(legacy)}}, sorted(legacy))
