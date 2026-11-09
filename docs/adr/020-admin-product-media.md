# ADR 020: Product media management in admin

Status: Implemented for Phase C, slice C2. Date: 2026-10-30. Extends [ADR 019](019-managed-product-media.md), [product revisions](013-staff-product-management.md) and the [B3 editor](015-admin-product-ui.md). C3 is not included.

## Product media and legacy coexistence

The staff product DTO keeps the original `image` and `gallery` fields unchanged. Optional `managedMedia` contains a nullable primary and an ordered gallery. Each item exposes only `id`, `url`, `width`, `height`, `contentType` and `position`. Neither storage paths nor digests are part of this projection. Tombstones are excluded.

The projection is read in the same PostgreSQL statement as the product revision, including paginated admin lists. This avoids both per-product queries and a revision from one snapshot paired with references from another.

`domain.ResolveProductMedia` is the single customer resolution rule, applied by the PostgreSQL product reader and checkout:

| Live managed references | Customer primary | Customer gallery |
| --- | --- | --- |
| None | Original `products.image` | Original `products.gallery`, with its existing semantics |
| Primary only | Managed primary URL | Empty; no legacy gallery fallback |
| Gallery only | Original `products.image` | Managed gallery in position order |
| Primary and gallery | Managed primary URL | Managed gallery in position order |

Detaching every managed reference restores the legacy fallback until the product is migrated; [ADR 021](021-legacy-media-migration.md) retires the fallback per product and adds the marker that records it. This cutover avoids bringing old gallery images back when a managed primary has deliberately been given an empty gallery.

Public product DTO `image` and `gallery` are the resolved values. Their field names and types remain compatible; the optional `managedMedia` field makes the managed identity explicit. The shared reader covers catalog, product detail, home/collection lists, favorites, cart, checkout review, search suggestions and current-product data in repeat-order previews. Frontend components consume these resolved URLs without maintaining a second selection rule. Historical order lines and repeat source lines continue to use their own snapshots.

No legacy image/gallery row is rewritten, no legacy file is copied into managed storage, and `web/assets` remains intact.

## Atomic staff operation

`PUT /api/admin/v1/products/{product}/media` accepts exactly:

```json
{
  "expectedRevision": 7,
  "primaryId": null,
  "galleryIds": ["med_0123456789abcdef0123456789abcdef"]
}
```

All members are required. `primaryId` is a live media ID or `null`; `galleryIds` is an array of at most 20 distinct live IDs, excluding the primary. Missing/deleted assets and invalid IDs return `422 validation_error` with stable `primaryId`, `galleryIds` or `expectedRevision` fields. The database constrains stored content types to normalized JPEG/PNG. Unsupported originals cannot enter the asset store through upload.

The operation locks/checks the expected product revision, locks the selected live assets, replaces all references, increments revision exactly once, and commits exactly one `product.media_update` audit event. The response contains the exact committed revision and selected IDs, rather than rereading a potentially newer product after commit. `409 stale_revision` changes neither references, revision nor audit. Archived products remain editable, matching B1.

Staff identity comes from the existing session. The normal trusted Origin, CSRF and expected-staff checks apply; the UI always sends `X-Expected-Staff-ID`. The shop edge still returns 404 for this route. No new authentication mechanism or public admin exposure is introduced.

## Editor behavior

The B3 product form contains a media section with managed previews, upload, primary selection, a small paginated live-asset picker and keyboard-accessible gallery up/down buttons. Upload accepts JPEG/PNG/WebP up to the C1 10 MiB limit. Extension/size checks are convenience checks; server decoding remains authoritative. The busy status covers upload/normalization rather than inventing a percentage.

Media selection and order are local drafts until **Сохранить изображения**. Product fields and media save separately against the same revision. Unsaved changes in one section block editing/saving the other, and pending uploads block product saves. This prevents one response from overwriting another section's pending state. Stale media saves retain selections and expose the existing explicit reload action. Reload adopts the authoritative product and media together.

A successful upload remains in the library even if a later attachment save fails. Upload, save, storage, session, validation and deletion failures do not clear unrelated drafts. A completed upload is not automatically attached or deleted. Legacy paths remain visible as text on the admin host; legacy `/assets` serving on that host remains blocked by the existing host boundary. Managed images have previews through `/media`.

**Убрать из товара** only changes references in the draft. **Удалить файл…** is a separate confirmed global operation, available after removing the image from the draft. If the server still references it, deletion is refused until detachment is saved. Product/order reference counts explain refusal without identifying any customer or order. Changing primary never deletes the former primary.

## Checkout and historical retention

New checkout records the resolved primary URL in `order_items.image`, including `/media/{id}`. The ID is immutable, so subsequent product edits do not change the order-time image identity. Legacy `/assets/...` snapshots remain untouched.

Global deletion checks both distinct product references and distinct historical order references before tombstoning. Any reference returns `409 reference_conflict` with safe counts (`products`, optional `orders`) and no audit or filesystem mutation. Live media list/get adds `orderCount`. Canceled and fulfilled orders retain their snapshots too; there is no status-based retention exception. C1 reconciliation therefore never treats a retained order image as a tombstoned object to unlink.

Migration **018_order_media_index** adds only a partial `(image, order_id)` index for `/media/%` order snapshots. Lookup uses exact URL equality and the matching partial predicate. No table or snapshot values change; migrations 001–017 remain byte-identical.

## Locking and race safety

| Write | Lock order |
| --- | --- |
| Upload | Staff actor → new asset → audit |
| Product media | Staff actor → product `FOR UPDATE` → live assets `FOR SHARE` in ID order → replace `product_media` → revision/audit |
| Delete | Staff actor → asset `FOR UPDATE` → count product and order references → tombstone/audit |
| Checkout | Customer guard → selected products `FOR UPDATE` in ID order → selected cart rows → selected primary assets `FOR SHARE` in ID order → order snapshots/stock/cart → commit |

Delete never locks a product after an asset. Assignment cannot attach a tombstone: it either acquires its share lock first and deletion observes the reference, or deletion commits first and assignment fails validation. Checkout's product locks stabilize the selected media until commit, and asset share locks also serialize snapshot creation with deletion. Once checkout releases the locks, its historical reference protects the bytes even if the product changes primary. Real PostgreSQL races cover both assignment/delete and checkout/detach/delete.

## Compatibility and verification

The additive index is compatible with C1's migration runner and ordinary product writes. **Schema compatibility does not make a write-enabled C1 rollback safe after C2 orders exist:** C1 does not count order media references on deletion. Retain the C2 backend for order retention, or disable media mutations during such a rollback. The storefront can continue using the resolved `image`/`gallery` fields without requiring the new optional metadata.

Focused tests cover resolution/cutover, duplicate rejection, revision and audit behavior, live-only assignment, order retention and concurrency. Admin tests cover multipart/auth headers, preview, selection, reorder, detach, explicit deletion, conflict/reload and preservation after failure. Storefront tests cover managed URL rendering/order alongside legacy responses.

`scripts/test-admin-media-flow.mjs` runs the actual staff/browser and customer checkout journey with two admin tabs, three uploads, gallery reorder, stale reload, reference-safe delete, a historical snapshot after primary replacement, audit counts, and a newly inserted legacy-only fixture. It also captures 1440/768/375 px views into ignored `artifacts/c2/`. Run only on disposable data: it changes product p01, creates an order, and inserts a new legacy fixture. It supports the existing Compose setup with `with-admin-tls-proxy.sh`; `C2_PG_SOCKET` selects a disposable native PostgreSQL socket for local verification instead. The native stack uses the repository Nginx route templates and production Next builds, with only local listen/upstream addresses changed.
