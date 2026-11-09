# ADR 019: Managed product media storage

Status: Accepted for Phase C (slice C1). Date: 2026-10-24. Builds on ADR 012 (audit) and ADR 013 (product revisions). C2 connects it to the product editor; C3 migrates and retires legacy media.

> C2 extends the C1 decisions below: [ADR 020](020-admin-product-media.md) adds the editor/API, prohibits primary/gallery duplication, resolves customer media and protects order snapshots.

## Context: legacy media inventory

Before C1 no image was ever uploaded. Every image is a file shipped with the code:

| Legacy source | Physical location | DB representation | Serving path | Consumers |
| --- | --- | --- | --- | --- |
| 3 product photos (`product-chocolate`, `-giftbox`, `-marshmallow`), WebP VP8 lossy, 1254×1254, 54–163 KB | `web/assets/` in the repository, copied into the Go image (`/app/web/assets`) | `products.image` (text) and `products.gallery` (text[]) hold root-relative paths such as `/assets/product-chocolate.webp`; set only by `002_demo_seed` | Go's static handler (`WEB_DIR`, `Cache-Control: public, max-age=31536000, immutable`); the shop edge proxies `/assets/*` to Go | Storefront `<img>` everywhere (catalog card, product gallery, cart, search suggestions, order thumbnails); legacy `web/app.js`; B3 admin shows the paths read-only |
| Hero image | `web/assets/hero-confectionery.webp` and `apps/storefront/public/brand/` | none | Go static and Next public | Home pages |
| Collection images, manufacturer and brand logos | none (all empty) | `collections.image`, `manufacturers.logo`, `brands.logo` | — | Storefront home page (empty → no image) |
| Order line images | — | `order_items.image`: a copy of `products.image` taken at checkout | the product's path | Order history, repeat flows |

- **Images and code are one artifact.** No volume holds images; they are versioned and backed up with the code.
- **No Next `Image` optimization.** Both Next apps render plain `<img>` with the path Go returns.
- **Product lifecycle.** Products are archived, never deleted (ADR 013).

C3 needs this table.

## Decision

### Architecture

```
admin edge ─▶ Go /api/admin/v1/media ─▶ application.Media ─▶ media.Store (filesystem, MEDIA_ROOT volume)
                                                         └─▶ PostgreSQL media_assets + staff audit
shop/admin edge ─▶ Go /media/{id} ──────────────────────────▶ same
```

`internal/media` owns image validation, normalization and the store. It knows nothing about products, staff or SQL.

**The store boundary** has five operations: `Put`, `Open`, `Remove`, `Objects`, `RemoveTemp`. The filesystem is the only implementation. An object store could implement the same interface later without touching the domain.

**The Go server owns the bytes.** The Next apps never touch the filesystem; they load images over HTTP like any browser.

### Persistent storage

**Configuration.** `MEDIA_ROOT` names the store.
- **Empty (the default for `go run` and the memory store):** managed media is off. `/media/*` is 404 and the staff media routes do not exist.
- **Set:** it needs PostgreSQL, and startup fails unless the root is:
  - a clean absolute path other than `/`;
  - an existing directory, not a symlink;
  - owned by the server's uid;
  - not world-writable;
  - writable, checked with a probe file.

Nothing silently falls back to `/tmp`, the working directory or the image.

**Compose:**
- the app mounts the named volume `media_data` at `/var/lib/sweetshop/media`, with `MEDIA_ROOT` set accordingly;
- `MEDIA_ROOT=` disables managed media;
- the image creates that directory owned by uid 10001, so Docker initializes the empty volume writable;
- the image itself leaves `MEDIA_ROOT` unset;
- the app's root filesystem is read-only, so setting `MEDIA_ROOT` without a volume fails the write probe instead of writing into the container.

**Layout:** `objects/<hex 0–1 of the ID>/<ID>` for finished objects and `tmp/upload-*` for writes in progress. Paths are built only from a validated ID and fixed names. The original file name is not kept at all: it has no operator value and may contain personal data.

### Media ID

`med_` plus 32 hex digits (128 bits from `crypto/rand`), the same shape as staff and customer IDs.
- It is validated everywhere: by the application, by the store, and by a database check constraint.
- It never derives from a file or product name.
- It is never reused: rows are tombstoned, not deleted.

### Upload validation and normalization

| Rule | Value |
| --- | --- |
| Accepted input | still JPEG, PNG and WebP, identified by magic bytes |
| Refused (415 `unsupported_media_type`) | SVG, GIF, HTML or anything else; animated PNG (`acTL`); animated WebP (VP8X flag) |
| File size | ≤ 10 MiB (413 `media_too_large`) |
| Request body | ≤ file + 64 KiB in Go (`MaxBytesReader`); ≤ 11 MiB at the admin edge, on this route only (413 from nginx). Every other admin route keeps nginx's 1 MiB. |
| Dimensions | ≤ 8192 px per side and ≤ 16,777,216 px total (422 `validation_error` on `file`) |
| Concurrency | at most 2 normalizations at once |

**Why these limits.** Catalog images are 1254×1254 and under 200 KB. The pixel limit bounds one decode at 64 MiB of RGBA, and with two at a time memory stays bounded. Dimensions come from the image header **before** any decoding, so a decompression bomb never gets a pixel buffer. A corrupt or truncated image is 422, never 500.

**Normalization.**
1. Decode the image.
2. Apply a JPEG's EXIF orientation to the pixels (EXIF itself is dropped).
3. Re-encode it:
   - **JPEG** at quality 88 when every pixel is opaque;
   - **PNG** when the image has transparency, so alpha is preserved.

Re-encoding drops EXIF, ICC and any other metadata, trailing payloads and polyglot tricks. The stored content type is always server-derived. Go has no WebP encoder, so WebP uploads become JPEG/PNG: the files are larger, but the format is controlled. A future slice may add a WebP or AVIF encoder.

Only `image/jpeg` and `image/png` can ever be stored; a database check enforces it. The client's file name and declared type are ignored.

**Dependency.** `golang.org/x/image` v0.23.0, for the WebP decoder only. This version still supports Go 1.22 and needs no other dependency bump.

### Metadata: migration `017_managed_media`

`media_assets` holds:
- `id`;
- `content_type`;
- `size_bytes`;
- `width` and `height`;
- `sha256`, all of the **stored** bytes;
- `created_at`;
- `deleted_at`, the tombstone.

Check constraints cover the ID format, content type, positive sizes and the digest format. Bytes are never stored in PostgreSQL. The creator is recorded in the audit row, not duplicated here.

**Bytes and metadata never change under an ID.** There is no PUT: a new image is a new asset.

### Upload atomicity and failure model

PostgreSQL and the filesystem cannot commit together, so the order is fixed:

1. Read the bounded body; decode, validate and normalize in memory.
2. Choose a fresh ID.
3. Write `tmp/upload-*`, fsync it, then hard-link it to `objects/../<ID>` and fsync the directory. A link fails if the target exists, so an object is never overwritten, and it appears complete or not at all.
4. In one `InStaffMutation` transaction, insert the row and the `media.upload` audit event.
5. If that transaction fails (including an audit failure), remove the object best-effort.

**A row therefore always names bytes that were written completely.** A crash between steps 3 and 4 leaves an orphan file, which reconciliation handles.

| Failure | Result |
| --- | --- |
| Root unavailable at startup | the server refuses to start |
| Write, fsync or link failure | 503 `storage_unavailable`; no row; the temporary file is removed |
| DB/audit failure | 500; no row; the object is removed (or left as an orphan) |
| Live row, file missing | `/media` answers 404 and logs an error; reconciliation reports it |
| File size differs from the row | 404 and an error log |
| Unlink fails after a delete commits | the delete stands; the leftover goes to reconciliation |

No error response contains a path, an errno or SQL.

### Reconciliation: `shopctl media verify [--grace=24h] [--apply]`

It lists the store first and the rows second, so an upload committing in between counts as known. It then classifies:

| State | Meaning | `--apply` |
| --- | --- | --- |
| row + file | healthy | — |
| live row, no file | **missing** (exit 3) | reported only; the bytes cannot be recreated |
| file, no row, older than grace | **orphan** | removed |
| file, no row, younger | young orphan (possibly an upload in progress) | kept |
| deleted row + file | **deleted** leftover | removed |
| `tmp/upload-*` older or younger than grace | stale or young temp | stale removed |

**Safety rules for `--apply`:**
- it needs a grace period of at least one hour;
- it only ever touches names in the store's own layout (`objects/<2 hex>/med_…` in the matching shard, `tmp/upload-*`); anything else in the tree is invisible to it;
- it refuses to remove orphans when the database has no media rows at all, which most likely means the wrong `DATABASE_URL`.

Nothing runs automatically at startup. Operators run it on demand or on a schedule.

### Product reference model

`product_media(product_id, media_id, role, position)`:
- `role` is `primary` or `gallery`;
- a primary must be at position 0, and the primary key `(product_id, role, position)` therefore allows one primary per product;
- gallery positions are unique, and the order is deterministic;
- an asset appears at most once per role, but it may be both the primary and a gallery image (the seed data does exactly that);
- foreign keys point to `products` and `media_assets`; neither is ever deleted.

**The C2 primitive.** `Media.SetProductMedia(mutation, expectedRevision, {productID, primaryID, galleryIDs})` replaces a product's managed images. It covers attach, reorder and detach, with at most 20 gallery images, audited as `product.media_update`. It bumps the product revision, so a concurrent B1 edit based on the old state is refused. **C1 exposes no route for it.**

### Deletion

`DELETE /api/admin/v1/media/{id}`, in one transaction:
1. Lock the row `FOR UPDATE`.
2. Count the products that reference it. Any reference is `409 reference_conflict` with `error.references.products`, and nothing changes. No cascade.
3. Otherwise set `deleted_at` and write the `media.delete` audit event.

After commit:
- the bytes are unlinked; a failed unlink does not roll the tombstone back;
- `/media/{id}` is 404 at once;
- the staff read and list hide the asset;
- `SetProductMedia` refuses it as a missing asset;
- the ID is never reused.

### Lock order

| Path | Locks, in order |
| --- | --- |
| Upload | staff row → insert asset |
| Delete | staff row → asset `FOR UPDATE` → count `product_media` |
| Product media | staff row → product `FOR UPDATE` → assets `FOR SHARE`, ID order → replace `product_media` rows |
| Product write (B1) | staff row → product → … (unchanged) |

Delete's row lock conflicts with the share lock (and the foreign key's key-share lock) an attachment holds:
- **delete commits first:** the attachment sees the tombstone and fails validation;
- **attachment commits first:** the delete counts the reference and refuses.

No path locks a product after an asset, so no cycle forms. A real-PostgreSQL test races the two for ten rounds, observing both winners and never a reference to a deleted asset.

### Audit

| Action | Target |
| --- | --- |
| `media.upload` | `media/<id>` |
| `media.delete` | `media/<id>` |
| `product.media_update` | `product/<id>` |

Each is exactly one row per committed mutation. Refusals write nothing:
- validation, size or type errors;
- authorization failures;
- reference conflicts;
- failed transactions.

Rows hold references only: no bytes, file names, EXIF or image metadata.

### Staff API (`api/admin-openapi.yaml` 0.7.0-c1)

`GET`/`POST /api/admin/v1/media` and `GET`/`DELETE /api/admin/v1/media/{id}`, behind the existing staff session, Origin, CSRF and expected-staff checks. Reads are authorized as `media.read`.

**Upload format.** `multipart/form-data` with exactly one part named `file`.

**Asset fields:**
- `id`;
- `contentType`;
- `sizeBytes`;
- `width` and `height`;
- `sha256`;
- `createdAt`;
- `url` (`/media/{id}`);
- `productCount`.

**List:** pages newest first. Deleted assets are never listed.

### Public delivery: `GET`/`HEAD /media/{id}`

The route is served by Go on both the shop edge and the admin edge (for previews). It is a resource like `/assets`, not an API operation, so it is outside both OpenAPI contracts.

**Edge rules:**
- only GET and HEAD pass (`limit_except`; anything else is 403 at the edge, 405 in Go);
- `/media` itself is 404 at the edge;
- other shapes under `/media/` are 404 in Go, never the legacy SPA fallback;
- the shop edge still returns 404 for `/api/admin/*`, and the admin edge for the customer `/api/*`.

**Response headers:**
- `Content-Type` from the row;
- `Content-Length`;
- `ETag: "<sha256>"`;
- `Cache-Control: public, max-age=31536000, immutable`;
- `X-Content-Type-Options: nosniff`;
- `Content-Security-Policy: default-src 'none'; sandbox`.

There is no `Content-Disposition` and no file name. `If-None-Match` answers 304, and HEAD has no body. Errors are `no-store`.

**Caching caveat.** The content behind an ID never changes, so long caching is safe. But deleting an asset does not purge copies browsers or proxies already hold: they may keep showing it until `max-age` runs out. There is no CDN today, hence no purge API.

### Backup and restore

A complete backup is now **PostgreSQL plus the `media_data` volume**. A database-only backup loses every uploaded image, and the rows would then point at missing files.

- **Order:** back up the database, then the volume. Files written after the dump are just orphans to the restored database, which reconciliation removes after the grace period.
- **Restore:** restore both from the same backup set, then run `shopctl media verify` (no `--apply`). It must report no `missing`.
- **Legacy images** still live in the repository and image (`web/assets`) until C3. They are versioned with the code, not part of the volume, and must not be deleted yet.

### Legacy coexistence and the C2/C3 boundaries

C1 changes no existing image:
- `products.image` and `products.gallery` stay authoritative for every product, and the customer API, storefront, cart, checkout, order snapshots and B3 admin read them as before;
- `product_media` exists, but nothing customer-facing reads it yet.

| Slice | Scope |
| --- | --- |
| C2 | Exposes attachment in the product editor and decides how managed images reach the customer DTO. Order snapshots copy image URLs at checkout, so C2/C3 must decide whether an asset referenced only by `order_items.image` may be deleted. |
| C3 | Migrates the legacy files into managed media and retires `web/assets` product photos and the legacy columns. |

### Rollback and mixed versions

Migration 017 only adds two tables.
- **B6 and older binaries** verify and migrate past it (tested). They ignore the tables, keep writing products (B1 writes don't touch `product_media`), and keep serving legacy images.
- **Unattached managed assets** are harmless to them. **`/media` stops working** while an older image runs, since older binaries have no such route, so nothing customer-facing may depend on it before C2's cut-over is final.
- **Mixed C1/B6 replicas:** uploads only reach C1 replicas, and a B6 replica answers `/media` with the legacy fallback.

The down migration drops both tables and leaves files alone; it is for development databases only.

## Not in C1

- product image UI, drag-and-drop, cropping;
- the HTTP attachment API;
- legacy migration or deletion;
- WebP/AVIF output, responsive variants;
- arbitrary file types;
- CDN or purge;
- RBAC, MFA, public admin exposure.
