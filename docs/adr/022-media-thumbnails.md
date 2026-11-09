# ADR 022: Managed media thumbnails

Status: Implemented for Performance P2, slices P2.1 (storage) and P2.2 (delivery, metadata, backfill, reconciliation). Date: 2026-11-10. Extends [ADR 019](019-managed-product-media.md) and [ADR 021](021-legacy-media-migration.md).

Product cards download full-size originals. Each managed image larger than a card needs a smaller derivative. P2.1 creates and stores that derivative. P2.2 serves it and backfills older assets; no storefront or admin UI uses it yet.

## Scope

P2.1: recipe and encoder, `media_thumbnails`, filesystem storage, new uploads, new `migrate-legacy` assets, deletion.

P2.2: the public route, API fields and OpenAPI, `shopctl media thumbnails`, thumbnail reconciliation in `media verify`.

Not yet: storefront and admin adoption. nginx needs no change: `location ^~ /media/` already reaches Go, for GET and HEAD only, on both hosts.

## Canonical input

The canonical input of every recipe is **the normalized original bytes, the ones `/media/{id}` serves**. An upload is normalized first and its thumbnail is derived from `Normalized.Data`, not from the pre-encode raster; `migrate-legacy` and any later backfill use the same function on the same bytes.

```
untrusted upload → media.Normalize → normalized original bytes → media.DeriveThumbnail
stored original bytes             ──────────────────────────────→ media.DeriveThumbnail
```

`DeriveThumbnail` takes only those bytes, so for the same original and recipe its bytes, metadata and SHA-256 are identical. The test `TestThumbnailIsDeterministicFromTheNormalizedOriginal` holds this for JPEG and transparent PNG, through a real store round trip.

Determinism holds for one build. A new Go toolchain or `x/image` version could change encoder output. The digest stays part of the immutable URL for that reason, and a recipe change gets a new identifier.

## Recipe `fit640-q80-v1`

| Property | Value |
| --- | --- |
| Longest side | 640 px, aspect ratio kept, integer rounding, each side at least 1 px |
| Never upscales | An original with longest side ≤ 640 has no thumbnail row or file |
| Resampling | `x/image/draw` Catmull-Rom into an RGBA target (premultiplied) |
| Opaque original (stored JPEG) | JPEG quality 80 |
| Transparent original (stored PNG) | PNG, default compression |

A stored PNG is by construction a transparent image (`Normalize` writes JPEG otherwise), so the output format follows the original. `x/image` is an existing dependency; no CGO.

The identifier is stored with every row. Any change to the table above needs a new identifier.

## Persistence

Migration `020_media_thumbnails` adds a 1:1 table keyed by `media_id` (foreign key to `media_assets`, which are never removed): `recipe`, `content_type`, `size_bytes`, `width`, `height`, `sha256`, `created_at`. A separate table keeps `media_assets` rows immutable and lets an asset without a thumbnail stay valid: older assets and images of at most 640 px simply have no row.

## Filesystem layout

```
MEDIA_ROOT/
  objects/<2 hex>/<ID>                 originals
  thumbnails/<2 hex>/<ID>/<SHA-256>    thumbnails
  tmp/upload-*                         writes in progress
```

Thumbnails stay **outside `objects/`**: every binary, including older ones, treats a `med_` object without a row as an orphan and removes it. The per-asset directory gives a future reconciliation one thing to compare with the rows and lets deletion work without knowing the digest. Names come only from a validated ID and digest. Writes use the same write-temp, fsync, hard-link sequence as originals, and never overwrite.

## Write order and failures

`Media.register`, shared by upload and `migrate-legacy`:

1. Derive the thumbnail under a normalization slot. A failure writes nothing.
2. Write the original.
3. Write the thumbnail, when there is one.
4. Commit the asset row, the thumbnail row and the one `media.upload` audit event in a single transaction.

No committed row can name bytes that were not written. On failure:

| Failure | Result |
| --- | --- |
| Original write | Nothing written, `storage_unavailable` |
| Thumbnail write | Original and partial thumbnail removed best effort, no row, `storage_unavailable` |
| Commit | Both files removed best effort, no row and no audit event |
| Crash before commit | Orphan original and an orphan `thumbnails/<2 hex>/<ID>` directory, both removed by `media verify --apply` after the grace period |

An orphan thumbnail file is harmless: nothing reads it, and no row names it.

There is no second audit event: the derivative is part of the one upload.

## Legacy migration

Only assets created by the run get a thumbnail. An asset reused by digest stays as it was and remains valid without one; a later backfill covers those. A dry run derives nothing.

## Deletion

The product and order reference checks are unchanged. Only after they pass, the delete transaction removes the `media_thumbnails` row together with the tombstone, and the thumbnail directory is unlinked after commit. A failed unlink logs a warning and leaves a directory with no row for reconciliation, like an original. A refused delete touches neither.

## Memory

Derivation runs in the same two normalization slots as `Normalize`, so at most two decoded images exist at once. `x/image/draw` allocates a resampling buffer of `targetWidth × sourceHeight × 32` bytes, so the worst case is a square 4096×4096 image (about 84 MB of the 250 MB below). Measured one at a time on 4096×4096 images (Go 1.22.1, amd64):

| Image | Step | Peak RSS | Wall time |
| --- | --- | --- | --- |
| Transparent PNG, 9.9 MB | Normalize | 102 MB | 5.9 s |
| Transparent PNG, 9.9 MB | Derive | 250 MB | 0.83 s |
| Opaque JPEG, 1.6 MB | Normalize | 33 MB | 0.44 s |
| Opaque JPEG, 1.6 MB | Derive | 140 MB | 0.73 s |

Two concurrent worst-case derivations would stay around 500 MB. The limit stays at 2: uploads are staff-only, and the catalog's 1254×1254 images are far smaller. If the application container ever gets a memory limit near that size, this needs reviewing.

## Rollback

Migration 020 is additive. The migrator tolerates versions newer than its own files, older binaries never read `media_thumbnails`, and older reconciliation lists only `objects/` and `tmp/`. An older binary therefore keeps serving originals and leaves thumbnails alone. Its deletes tombstone an asset without touching the thumbnail, leaving a row and directory of a deleted asset; the serving route requires a live asset, so neither is ever served, and reconciliation removes the directory.

P2.2 adds no migration. A deployment needs no ordering: before any backfill the API simply has no `thumbnailUrl`, a partly backfilled library reports it per image, and originals never depend on it.

## Delivery

`GET`/`HEAD /media/{id}/thumbnail/{sha256}`, served like `/media/{id}`. A thumbnail is public only if all of these hold, in this order:

1. the ID and digest have the exact format, or 404 without any lookup;
2. the asset exists and is not tombstoned, and a row exists for it (one query joining the two);
3. the URL digest equals the row's current `sha256`;
4. the file exists and has the row's size.

The filesystem never authorizes anything. A row of an older recipe is served until it is replaced, because its URL is still the one the API returns.

The URL is immutable. A digest that is not the current row's is 404, never a redirect, even while its file is on disk. A browser or CDN may keep a copy already cached under an old URL, which is the caveat `/media/{id}` already has (ADR 019). Successful responses use the digest as `ETag`, `Cache-Control: public, max-age=31536000, immutable`, `X-Content-Type-Options: nosniff` and the media CSP. Every error is `no-store`. A row whose file is missing or the wrong size is 404 and logged; a storage failure is 503.

## API metadata

`thumbnailUrl` on `ProductMediaItem` and `MediaAsset`, `thumbnail` on `SearchSuggestion`: always optional, and absent rather than null. Reads take it from the same statement as the image, by a `LEFT JOIN` or scalar subquery on the 1:1 table, so a page of products costs the same number of queries with or without thumbnails. No existing field changes meaning.

The URL says a current row exists, not that the file does; a client keeps the original as its fallback. A legacy `/assets` image, a managed image of at most 640 px and an asset not yet backfilled have none. The memory adapter has no managed media, so it never returns them.

`order_items.image` is untouched: checkout snapshots the original `/media/{id}` and never a thumbnail URL.

## Backfill

`shopctl media thumbnails [--apply] [--limit=N] [--timeout=30m]`. Dry run by default: it reads and checks files, derives and writes nothing. It is an operational derivation, not a staff action: no staff account, audit event or product revision.

| Asset | Result |
| --- | --- |
| Longest side at most 640 px | `notApplicable`; no file or row |
| Current recipe, file matches row | `complete` |
| No row | `generated` |
| Older recipe, or file missing or corrupt | `regenerated` |

Each asset stands alone:

1. Read the stored original and prove its size and SHA-256 against `media_assets`. A damaged original is reported and never derived from.
2. Derive with the same `DeriveThumbnail`, in a normalization slot and outside any transaction.
3. Write the file: write-temp, fsync, atomic rename. A digest names one content, so replacing an intact file changes nothing and replacing a damaged one repairs it.
4. Upsert the row in one statement that share-locks the live parent. Only that row is locked, and only for that statement. The lock conflicts with a delete's `FOR UPDATE`: a delete that wins leaves no live parent, and the file is removed best effort; a save that wins is deleted by the delete.

The previous file of a regenerated row stays until reconciliation. A crash between the file and the row leaves a file the next run derives again. One failed asset is reported and the next proceeds. `--limit` counts images generated, not inspected or failed, so a rerun always gets further.

Exit codes: `0` nothing left, `3` work remains, some failed or `--limit` stopped the run, `1` failure, `2` usage.

## Reconciliation

`shopctl media verify` keeps its meaning for originals and adds thumbnails. A live row's file is read and hashed; a file that is not current is judged by its name and age.

| State | Meaning | Non-zero exit? | `--apply` |
| --- | --- | --- | --- |
| `healthyThumbnails` | Row and file match | No | Kept |
| `pendingThumbnails` | Live, over 640 px, no row | No, informational | Kept |
| `outdatedThumbnails` | Row of an older recipe, still served | No, informational | Kept |
| `missingThumbnails` | Row without file | Yes | Reported; backfill restores |
| `corruptThumbnails` | File differs from row, or is unreadable | Yes | Reported; backfill restores |
| `deletedThumbnails` | File of a tombstoned asset | Yes | Removed at once |
| `orphanThumbnails` | File no row names, past grace | Yes | Removed |
| `youngOrphanThumbnails` | The same, inside grace | No | Kept |
| `staleThumbnails` | File that is not its row's current digest, past grace | Yes | Removed |
| `youngStaleThumbnails` | The same, inside grace | No | Kept |

`--apply` only removes filesystem debris; it never derives an image and never writes to the database. After `--apply`, missing and corrupt thumbnails still give exit 3 until `media thumbnails --apply` runs.

A database read that fails removes nothing. Orphans are kept, with `refused` set, when the database has no media rows at all or no thumbnail rows at all: an empty or restored database must not look like permission to wipe the store. Files of a tombstoned asset are still removed, because a tombstone is positive evidence.

Do not run `verify --apply` during a backfill: an orphan file about to gain its row could be removed. The next verify reports it as missing and the next backfill restores it.

Run the `020` down migration only when rolling the schema back in development; thumbnail files are left behind and are harmless.
