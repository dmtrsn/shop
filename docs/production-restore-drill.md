# S1.2: isolated restore and integrity drill

This command consumes S1.1 `database.dump`, `media.tar`, `manifest.json`, `SHA256SUMS`
and `LOCAL_COMPLETE` directly. It supports formatVersion 1 only. `LOCAL_COMPLETE`
proves local publication, and `offHost.status=not_verified` remains acceptable for
this local drill. It does not prove off-host recovery. No backup format changes.

## Invocation

Use Python 3.9+ on Linux with a local Unix-socket Docker daemon and Docker Compose.
Preload a PostgreSQL image of the backup's major version (with pg_trgm available)
and the matching repository application image. Nothing pulls, builds, or migrates.
The runtime image must contain `/app/sweetshop`, `/app/web/assets`, `/bin/sh`, `id`,
and run as a non-root user. The PostgreSQL helper needs `tar`, `find`, `chmod` and
`chown`. PostgreSQL images with a data volume at `/var/lib/postgresql/data` are
supported. Driver options, remote daemons and source volumes without matching
Compose project labels are refused.

Run `free -h`, `df -h /home /`, `uptime` first. The tool requires available RAM
of at least 1.5 GiB, /home free space of at least 1.5 GB and / free space of at
least 2 GB, plus conservative additional disk headroom on both filesystems:
four times the dump + archive sizes, plus 256 MiB. Actual expanded DB/media may
need more; check actual capacity before proceeding. Do not remove unrelated data
to make room. The restored media export is retained for diagnosis.

Example with placeholders (replace every identity and use a unique project):

```sh
scripts/restore-production-drill.py \
  --backup-dir /private/backups/BACKUP_ID \
  --environment restore-test --disposable \
  --project drill-UNIQUE \
  --compose-file /private/drills/NEW_WORK_DIRECTORY/compose.json \
  --database-service postgres --database-name drilldb --database-user drilluser \
  --database-volume drill-UNIQUE_database --media-volume drill-UNIQUE_media \
  --source-project SOURCE_PROJECT --source-database-volume SOURCE_DATABASE_VOLUME \
  --application-image MATCHING_LOCAL_APPLICATION_IMAGE \
  --application-git-sha FULL_40_CHARACTER_RELEASE_SHA \
  --postgres-image MATCHING_LOCAL_POSTGRES_IMAGE \
  --port 18082 --read-sessions-file /private/drills/read-sessions.json \
  --report /private/drills/restore-report.json
```

Project and volume identifiers in the real command must be lowercase; `UNIQUE`
above is a placeholder. The project must start with `drill-`. Target volumes must
start with that project plus `_`; the target DB name must differ from the source.
`NEW_WORK_DIRECTORY` must not exist; its parent must exist. The report must be
new, outside both backup and work directories, with an existing parent. Outputs
and source storage cannot overlap. Work files are private (umask 077); Compose
contains a newly generated disposable DB password and must stay private.

S1.1 records the media volume identity and database name, **not** the source
project or source PostgreSQL volume. Supply those two identifiers from the source
deployment inventory. The tool verifies their volume project labels against
Docker, records this provenance, and compares actual source/target mountpoints.
It requires retained source volume metadata; offline recovery on a different host
is outside this narrowly supported drill topology.

The current tracked checkout must be clean and have the recorded application SHA.
The application image must match the manifest digest; if no digest was recorded,
it must have `org.opencontainers.image.revision` equal to that SHA (for example,
set it with `docker build --label org.opencontainers.image.revision=FULL_SHA`).
Images are inspected and then pinned to their local image IDs. A controlled
`--allow-release-mismatch` override is reported and keeps recovery `INCOMPLETE`
when checkout/image identity does not match. Matching image assets are checked
even under that override; it cannot excuse a missing legacy file.

The optional session file must be a private, operator-owned regular file with
only `customerCookie` and `staffCookie` string fields. Each value is the complete
Cookie header for a suitable existing fixture session in the backup. Use an
isolated fixture with valid, unexpired customer/staff cookie sessions. Cookies
are never printed or written to reports. The drill performs no login, inserts,
checkout or order mutations. Missing cookies give `INCOMPLETE`; rejected cookies
give `FAIL`. Authentication failures are not successful admin/cart/order reads.

## Safety and verification order

1. Validate marker, manifest structure, version, fixed filenames, artifact sizes,
   all three SHA256SUMS entries, and dump/archive hashes recorded by the manifest.
   Symlink artifacts, duplicate checksum entries and unsafe names are rejected.
2. Read every tar entry and payload before creating restore resources. Require
   complete archive end markers. Reject absolute paths, traversal, duplicate
   normalized paths, file ancestors, symlinks, unsafe/forward/cyclic hardlinks,
   devices/FIFOs, privileged modes and sparse entries. Internal hardlinks may
   point only to a previously checked file (publication can leave a tmp hardlink).
   No blind unvalidated tar
   extraction. Backups must remain unchanged for the duration of the drill.
3. Refuse production environment names, source project/volume reuse, any existing
   target volumes/network/project, conflicting existing ports and occupied host
   ports. Create an internal backend bridge, new local volumes and a DB without
   a host port. Only the app also joins a separate, invocation-owned ordinary
   bridge for `127.0.0.1:<port>:8080` publication. An internal-only bridge can
   retain a requested HostConfig binding without actually publishing it.
   Direct container-IP HTTP would require changing the localhost read interface
   and relying on host routing to Docker subnets, so it is not used.
   The publication bridge gives the app a default route and outbound connectivity;
   this topology does **not** provide full application egress isolation. PostgreSQL
   stays on the internal backend only; email/SMTP remain disabled.
   Compose cannot inherit host `.env` or Compose overrides. All helpers use
   `--network none`; only new target media is mounted for writing.
4. Inspect labels, volume driver/options, actual mountpoints, container mounts and
   network IDs before restoring. No source DB connection is made. Wait for fresh
   PostgreSQL, assert no user tables/views/sequences, and require the same major.
   Restore with `pg_restore --exit-on-error --single-transaction --no-owner
   --no-privileges` into the newly initialized disposable role/database. No
   `--clean`, DROP or source database create/drop command is emitted.
5. In a read-only transaction compare all eight representative counts and the
   ordered migration marker with the manifest, and require pg_trgm. Never repair
   or apply migrations. Schema incompatibility is fatal.
6. `restore_drill_integrity.py` is a reusable read-only helper. It proves every
   live original's existence, recorded size and SHA-256, and the same for every
   recorded live thumbnail. It checks the validated backup archive first and an
   independent tar export of the restored volume next. The exported file
   inventory must exactly match backup paths, sizes and hashes, including extras.
   Extraction requires empty media; ownership and modes become usable by the
   image's actual non-root UID/GID (directories 0750, files 0640). No bytes or
   relative paths are repaired, regenerated, cleaned or rewritten.
7. Report unreferenced originals, tombstoned leftovers, stale thumbnails,
   tmp/upload leftovers and other extras separately. These alone do not fail.
   Unrecorded thumbnails are not regenerated; all recorded live thumbnails must
   match. Check primary/gallery association semantics and every referenced live
   managed asset, including references in product compatibility columns.
8. Check every historical image and product image/gallery reference. Managed
   historical URLs must identify a live original/current recorded thumbnail;
   legacy paths must be safe `/assets/...` paths. Require `/app/web/assets` and
   every referenced legacy file in the same release image, with no host copies.
9. Start the restored app only after those checks pass. It uses an explicit
   target DB URL, production mode, `RUN_MIGRATIONS=false`, disabled email and
   cleared SMTP/origin configuration, a read-only root filesystem, and a literal
   loopback port. Its DB sessions default to read-only transactions. Confirm its
   actual image, env, mounts, both network IDs and their exact participants after
   start. Recheck that PostgreSQL has no published ports and no publication-network
   attachment. Require the exact loopback binding in both HostConfig.PortBindings
   and runtime NetworkSettings.Ports; a correct request with empty runtime ports,
   wildcard address, wrong port, duplicates or another published port fails.
   Subsequent HTTP health checks also require actual host reachability.

   Docker's own host firewall/routing behavior remains a dependency. In particular,
   [Docker documents](https://docs.docker.com/engine/network/port-publishing/)
   same-L2 access to localhost-published ports on Engine versions before 28.0.0.
   These metadata/HTTP checks do not prove protection against that engine issue;
   do not treat a loopback binding as an external-network penetration test.
10. Read health, catalog, a product detail, a product image, admin products, cart,
    orders, and a historical image when present. Require HTTP 200, JSON where
    applicable, and matching managed image digests. Redirects and environment
    HTTP proxies are disabled. No production URL can be used as a read target.
    An empty product catalog/no product image leaves those reads `INCOMPLETE`;
    no historical images is a checked empty set. Compare all ordered historical
    image values and media/reference metadata before and after app reads.

S1.1 does not include an independent per-row historical snapshot digest. The
drill proves use of the checksummed dump and unchanged historical values after
restore, rather than claiming a separate row-for-row source comparison. A tiny
fixture with source mutation after backup is required as real boundary evidence.
Legacy existence alone cannot establish release identity or bytes against a
historical manifest: the retained matching release/digest supplies that dependency.

## Report and exit status

`restore-report.json` has formatVersion 1 and includes `backupId`, `sourceRelease`,
`targetProject`, UTC start/end and elapsed monotonic seconds, `artifactChecksums`,
`databaseRestore` (version, times, result, ownership and verify-only migration
policy), `countComparison`, `mediaIntegrity` (originals, thumbnails, extras,
product/historical references, legacy files), `historicalSnapshots`,
`applicationReads`, `isolation`, `resources`, `checks`, `cleanup` and `verdict`.
Unreached checks stay `PENDING`. Failure/limitation fields contain fixed diagnostic
messages, never command stderr, credentials, customer rows or HTTP bodies. Counts,
release/resource IDs and source mountpoints are operational metadata; keep reports
private. Validation may refuse unsafe report destinations before producing a file.

- `PASS` / exit 0: every mandatory check passed with matching release identity.
- `FAIL` / exit 1: a safety, integrity, database, reference or application check failed.
- `INCOMPLETE` / exit 3: required checks could not run, including denied Docker,
  insufficient resources, missing suitable sessions or controlled release mismatch.
- Exit 2: invalid/unsafe CLI or report destination; no drill verdict is claimed.

Failure takes precedence over incomplete checks. Skipping Docker/app verification
never produces PASS. Duration is reported for failures and incomplete attempts too.
Resources are **always kept**, even after success; there is no cleanup option.
Never prune or use an ambiguous Compose `down -v`. Any later manual cleanup must
verify exact invocation IDs and `shop.restore-drill` labels first.

## Evidence required before closing F01

```sh
python3 -m unittest discover -s tests/restore -v
python3 -m unittest discover -s tests/backup -v
make check
make check-api
git diff --check
```

Unit tests use command-boundary fakes, including restore from backup state A
after source becomes B, checksum/archive/hash failures, original/thumbnail
damage, failed pg_restore/app reads and isolation violations. This is not
end-to-end PostgreSQL or Docker evidence.

On a Docker-capable host with sufficient resources, create a tiny **new** source
fixture project with managed originals, recorded thumbnails, product associations,
legacy/managed order snapshots and suitable read sessions. Use the S1.1 command
to take the matched backup at A; then change that fixture's DB/media to B. Run
this command into a second new project and prove all restored counts/hashes/
references and read results equal A, not B. Retain the JSON report and elapsed
time. No existing dev/production data may participate. Until this real drill
passes, F01 remains open. S1.3 off-host verification, retention and scheduling are
separate work.
