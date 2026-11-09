# ADR 022: Managed media thumbnails

Status: Implemented for Performance P2, slice P2.1 (storage only). Date: 2026-11-10. Extends [ADR 019](019-managed-product-media.md) and [ADR 021](021-legacy-media-migration.md).

Product cards download full-size originals. Each managed image larger than a card needs a smaller derivative. P2.1 creates and stores that derivative; nothing serves it yet.

## Scope

P2.1: recipe and encoder, `media_thumbnails`, filesystem storage, new uploads, new `migrate-legacy` assets, deletion.

Not in P2.1: the public route, API fields and OpenAPI, backfill of existing assets, thumbnail reconciliation in `media verify`, storefront, admin and nginx changes. Until those exist, thumbnails are written but never read.

## Canonical input

The canonical input of every recipe is **the normalized original bytes, the ones `/media/{id}` serves**. An upload is normalized first and its thumbnail is derived from `Normalized.Data`, not from the pre-encode raster; `migrate-legacy` and any later backfill use the same function on the same bytes.

```
untrusted upload → media.Normalize → normalized original bytes → media.DeriveThumbnail
stored original bytes             ──────────────────────────────→ media.DeriveThumbnail
```

`DeriveThumbnail` takes only those bytes, so for the same original and recipe its bytes, metadata and SHA-256 are identical. The test `TestThumbnailIsDeterministicFromTheNormalizedOriginal` holds this for JPEG and transparent PNG, through a real store round trip.

Determinism holds for one build. A new Go toolchain or `x/image` version could change encoder output. The digest stays part of the planned immutable URL for that reason, and a recipe change gets a new identifier.

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
| Crash before commit | Orphan original (removed by `media verify --apply` after its grace period) and an orphan `thumbnails/<2 hex>/<ID>` directory |

An orphan thumbnail directory is harmless: nothing reads it, and it has no row to name it. Removing them is left to the later reconciliation slice.

There is no second audit event: the derivative is part of the one upload.

## Legacy migration

Only assets created by the run get a thumbnail. An asset reused by digest stays as it was and remains valid without one; a later backfill covers those. A dry run derives nothing.

## Deletion

The product and order reference checks are unchanged. Only after they pass, the delete transaction removes the `media_thumbnails` row together with the tombstone, and the thumbnail directory is unlinked after commit. A failed unlink logs a warning and leaves a directory with no row for later reconciliation, like an original. A refused delete touches neither.

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

Migration 020 is additive. The migrator tolerates versions newer than its own files, older binaries never read `media_thumbnails`, and older reconciliation lists only `objects/` and `tmp/`. An older binary therefore keeps serving originals and leaves thumbnails alone. Its deletes tombstone an asset without touching the thumbnail, leaving a row and directory of a deleted asset for later reconciliation; the serving route must require a live asset.

Run the `020` down migration only when rolling the schema back in development; thumbnail files are left behind and are harmless.
