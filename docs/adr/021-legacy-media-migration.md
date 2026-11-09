# ADR 021: Legacy product media migration and retirement

Status: Implemented for Phase C, slice C3. Date: 2026-11-09. Extends [ADR 019](019-managed-product-media.md) and [ADR 020](020-admin-product-media.md).

## Legacy media inventory

| Legacy path | Owning field | Physical file | Consumers | Migrate? | Retain? |
| --- | --- | --- | --- | --- | --- |
| `/assets/product-chocolate.webp`, `-giftbox`, `-marshmallow` | `products.image`, `products.gallery` | `web/assets/` in the repository and the Go image (`/app/web/assets`) | Catalog, detail, cart, checkout, favorites, search, repeat preview via the shared product reader; admin editor | Yes, per product | Yes: seeded order snapshots use them and `web/app.js` (`/legacy` storefront) names all three |
| Same paths | `order_items.image` | Same | Historical order lines | No: order history is immutable | Yes, while any order references them |
| `/assets/hero-confectionery.webp` | None | `web/assets/` | `web/styles.css` (legacy storefront); the Next home page uses `/brand/hero-confectionery.webp` | Not product media | Yes |

Other sources that could hold `/assets` paths — `collections.image`, `brands.logo`, `manufacturers.logo` — are counted by the retention report. The in-memory demo store keeps its seeded paths; it has no managed media and no migration.

## Migration strategy

`shopctl media migrate-legacy` is an operator command, not a SQL migration: legacy files live in the filesystem and must pass the same `media.Normalize` pipeline as uploads, and the normalized bytes reach the store through the same `Put` and asset registration as an upload.

Without `--apply` it is a dry run and changes nothing, in the database or in `MEDIA_ROOT`. It prints a JSON report and exits 3 while anything still needs attention (0 when nothing is left, 1 on failure, 2 on usage).

For each product, in ID order, one transaction at a time (also archived products):

1. Classify the product (table below).
2. Normalize the legacy columns: `products.image` first, then `products.gallery` in stored order, dropping empty entries and repeated paths. The image becomes the managed primary; the rest become the managed gallery in order. An empty `image` stays no primary. Images with identical bytes under different paths are one image; the later path is listed in `duplicates`.
3. Validate and normalize every file of the product **before storing any**. A missing, unreadable, invalid or unsupported file, a path that is not a single file directly under `/assets`, or more than 20 distinct gallery images leaves the product unchanged and marks it `failed` with a precise problem kind (`missing_file`, `unreadable_file`, `invalid_image`, `unsupported_image`, `unsupported_path`, `gallery_too_large`, `storage_error`, `database_error`). Failures never stop the other products.
4. Find or import each image: a live asset with the same normalized SHA-256 is reused (verifying its stored object), otherwise it is imported through `Media.register` with a `media.upload` audit event.
5. In one transaction, under the product row lock, re-check that the product is still legacy-only and its columns unchanged, attach the images (reusing `replaceProductMedia`), stamp `legacy_media_retired_at`, and bump the revision once.

The apply run takes a session-level advisory lock, so two operators cannot interleave.

| Status | Meaning | Action |
| --- | --- | --- |
| `needs_migration` | Dry run: legacy only, all files valid | Apply imports it |
| `migrated` | This run imported it | — |
| `already_migrated` | Fallback already retired | None |
| `already_managed` | Live managed primary, or managed gallery with no legacy primary | Apply only retires the fallback |
| `no_media` | No legacy and no managed images | None |
| `conflict` | Managed gallery but a legacy primary is still in use, or the product changed during the run | Choose a managed primary in admin, then rerun |
| `failed` | See problem kinds | Fix and rerun |

## Idempotency

Idempotency has two independent parts. `products.legacy_media_retired_at` (migration `019_legacy_media_cutover`, nullable, no default) records per product that the migration is finished, independent of whether the product later has images. Content hashes make assets converge: a run interrupted after an asset was stored finds it by digest. A crash between `Put` and registration leaves an orphan that `media verify --apply` removes after its grace period.

A rerun therefore leaves `media_assets`, `product_media`, revisions and audit unchanged, except for products that were fixed in the meantime.

## Already-managed products

Managed media from C2 wins and is never overwritten. Apply stamps the retirement marker on a product with leftover legacy data, without touching `product_media`, the revision or `updated_at`, and logs `product.legacy_media_retire`. A product with no legacy data is not touched at all.

The marker exists for this case: without it, staff detaching every image from such a product later would bring the legacy image back to the storefront, and a rerun would re-import it, undoing the edit.

## Customer cutover

`ResolveProductMedia` takes the marker, and is the single place the rule lives (product reader and checkout, read in the same statement):

| Marker | Live managed references | Primary | Gallery |
| --- | --- | --- | --- |
| Unset | As before (ADR 020) | Legacy fallback where ADR 020 uses it | Legacy fallback where ADR 020 uses it |
| Set | None | None | None |
| Set | Any | Managed primary if any | Managed gallery |

The fallback is deliberately retired **per product, when that product's migration commits**, not when the binary is deployed. A product that cannot be migrated — missing file, conflict, or not yet processed — keeps rendering its legacy path until the operator resolves it. Once every product has the marker no customer path reads the legacy columns; the fallback branch remains only as the rollback and not-yet-migrated path.

New orders record the resolved primary, so they snapshot `/media/{id}` for migrated products. Products without images remain valid and render the existing "no photo" states; no placeholder asset is invented.

## Order snapshots and legacy /assets retention

Historical `order_items.image` values are never rewritten, by the migration or anything else. The old `/assets/...` snapshots stay valid, so the files they name must remain served.

The report classifies every `/assets` file and referenced path:

| Category | Definition | Retained? |
| --- | --- | --- |
| `product` | An active (unretired) product references it | Yes |
| `historical_order` | Only historical orders reference it | Yes |
| `product_and_order` | Both | Yes |
| `other_consumer` | Static storefront or non-product catalog row | Yes |
| `unreferenced` | None of the above | Eligible for removal, reported in `safeToDelete` |

Files only named by retired products' compatibility columns are `unreferenced`, with their count in `compatibilityProducts`: they are not needed by any customer path, but removing them loses that part of a rollback (below). `safeToDelete` is empty unless the static web root (directory with `index.html` next to the assets) was scanned. A file name anywhere in the static storefront keeps the file; a false match only retains more.

Physical deletion is not automated. C3 deletes no file: on the current catalog `web/assets` has no removable file (three files back historical orders or the `/legacy` storefront, the hero backs its stylesheet), and if a future report offers some, removing them is a separate operator decision after backup. `/assets` routing therefore stays exactly as it was: Go `web/assets` static serving and the nginx `/assets` locations on the shop edge (not the admin edge).

## products.image / products.gallery

The columns stay as compatibility fields, read-only and no longer written by anything. Their removal would be irreversible and is not needed; it is a possible future cleanup. The admin DTO keeps them; `legacyMediaRetired` tells the editor to stop showing them. For unmigrated products the editor labels them compatibility-only.

## Audit

The operator names a real staff account with `--staff-email`; the command refuses an unknown or disabled account, and `apply` cannot run without it. It is the same trust model as `shopctl staff`: shell access to the deployment is the authorization. Each committed change logs one event with that staff actor and a `migrate-legacy:<timestamp>` request ID: `media.upload` per imported asset, `product.media_update` per migrated product, and `product.legacy_media_retire` per stamped already-managed product. The migration never invents an actor. A failed product logs nothing, since events are transactional.

## Backup requirement

Before `--apply` and before removing any legacy file, back up, in this order: PostgreSQL, the managed media volume (`media_data`), and the remaining legacy assets (`web/assets` or the image holding them). Removing legacy files additionally requires the migration and the cutover to be verified first.

## Rollback

- **Before legacy physical cleanup:** restore PostgreSQL and the media volume from the pre-migration backup, or per product `DELETE FROM product_media WHERE product_id=...` plus `UPDATE products SET legacy_media_retired_at=NULL WHERE id=...`. The legacy columns and files are intact.
- **After application cutover (binary rollback):** migration 019 is additive and older binaries ignore the column and keep working with the same `product_media` references, so the code can be rolled back. Older binaries restore the C2 detach fallback for products that later lost their managed images. Run the `019` down migration only when rolling the schema back.
- **After old files are deleted:** the legacy columns can no longer render those images. Only a restore from backup returns them. This is why C3 stops short of deleting anything.

## Fresh environments and fixtures

`002_demo_seed` keeps seeding legacy paths: migrations 001–018 are immutable, and a seed that wrote managed media would need the filesystem inside SQL. A demo environment reaches the post-C3 state by running `shopctl media migrate-legacy --apply --staff-email ...` once after `shopctl migrate`. The in-memory store keeps its seeded `/assets` paths. No production path depends on demo fixtures.

## Verification

Unit tests cover classification, content dedupe, the reusable-object check and the retention categories. PostgreSQL integration tests run the A–F scenario (legacy-only, already-managed, no image, legacy and managed order snapshots, missing file), dry run without mutation, apply, audit events, a second apply without changes, recovery after restoring a file, a detach staying honored, conflicts, the run lock and a disabled actor. `scripts/test-legacy-media-flow.mjs` runs `shopctl` and the browser journey on the real stack, and CI runs it.

## What remains after C3

- `web/assets` (all four files) and the `/assets` routes stay for historical orders and the `/legacy` storefront.
- The `products.image`/`products.gallery` columns stay as compatibility data.
- The fallback branch in `ResolveProductMedia` stays reachable only for products without the marker.
