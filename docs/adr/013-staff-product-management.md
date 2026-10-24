# ADR 013: Staff product management

Status: Accepted for Phase B (slice B1). Date: 2026-10-19. Builds on ADR 007 (staff identity) and ADR 012 (transactional staff audit).

## Context

B1 is the first staff business mutation. Staff can now change prices, stock and quantity rules that customers' carts and checkout depend on. So the product row needs a version, removal must keep history intact, and checkout has to stop trusting rules that were checked only when a line was added to the cart (the audit's P1).

## Decision

### Schema: migration `013_product_revisions`

`products` gains two columns. Nothing else changes, and existing IDs, slugs and SKUs are kept.

- `revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)`. Existing rows start at 1.
- `archived_at timestamptz` (NULL means active).

There is no hard delete. Carts and favorites reference products with `ON DELETE CASCADE`, and order lines are snapshots without a foreign key. Deleting a product would silently erase customer data, so products are archived instead.

### Revision

`revision` versions the whole product row. It increases by exactly one on:

- every staff create, update, archive and restore;
- every checkout stock decrement.

Every staff update, archive and restore carries `expectedRevision` in its JSON body. The store locks the row `FOR UPDATE` and compares the revision. A mismatch is `409 stale_revision`: nothing is written and no audit event is recorded. Process-local locks play no part.

Checkout bumps the revision because stock is staff-editable. If a sale didn't change the revision, an edit based on the pre-sale stock would overwrite the sale. The cost is that editors of fast-selling products sometimes have to reload. A future inventory-movement (delta) command can avoid that.

### API (`/api/admin/v1`, `api/admin-openapi.yaml`)

| Route | Action |
| --- | --- |
| `GET /products?status=active\|archived\|all&q=&page=&limit=` | list, ordered by name then ID; `limit` 1–100 |
| `GET /products/{product}` | detail, archived products included |
| `POST /products` | create, `201`, audited as `product.create` |
| `PUT /products/{product}` | full replacement plus `expectedRevision`, audited as `product.update` |
| `POST /products/{product}/archive` / `/restore` | `{expectedRevision}`, audited as `product.archive` / `product.restore` |
| `GET /catalog-options` | read-only dictionaries for the product form (added in B3, [ADR 015](015-admin-product-ui.md)) |

- **Protection.** Every route sits behind `staffAuth`: staff cookie only, staff Origin and CSRF on unsafe methods, optional `X-Expected-Staff-ID`. Reads go through `StaffAuth.Authorize(staff, "product.read")`. Mutations go through `staffMutation`, so the actor always comes from the session.
- **Bodies.** Every member is required and must be non-null. An omitted `stock` can never silently become 0.
- **Responses.** Every response returns the full `AdminProduct`, including `revision`, `archived` and `archivedAt`.
- **Errors.** `validation_error` (with fields), `not_found`, `stale_revision`, `conflict` (archiving an archived product, restoring an active one), plus the existing staff codes. Raw constraint errors are never exposed.

New products get `prd_` + 32 hex digits, the same convention as `stf_` and `usr_` IDs. Legacy IDs such as `p01` are unchanged.

### Invariants on every staff write

| Field | Rule |
| --- | --- |
| `priceMinor` | integer kopecks, 1 to 1 000 000 000 |
| `oldPriceMinor` | 0 (no old price) or above `priceMinor`, same bound |
| `stock` | 0 to 1 000 000; no backorders |
| `minimumQuantity`, `quantityStep` | 1 to 10 000 |
| text | trimmed; length-bounded; no control characters (descriptions may contain line breaks) |
| `slug` | `^[a-z0-9]+(-[a-z0-9]+)*$` |
| `sku` | `^[A-Za-z0-9][A-Za-z0-9._-]*$` |
| tags, badges, collections | at most 20 each, no repeats |

The bounds keep every order total inside int64. JSON numbers that are fractional, overflow or are strings are rejected (422).

These rules are enforced in Go only. Adding them as database `CHECK` constraints would make checkout's stock `UPDATE` fail on any legacy row that violates them. The existing non-negativity and `> 0` checks remain.

### References and identifiers

Category, manufacturer, brand and collections must exist, and the brand must belong to the manufacturer. That brand/manufacturer foreign key is deferred to commit, so it is checked explicitly first; a violation is a field error, not a commit failure.

Customer lookups accept a product's ID, slug or case-insensitive SKU. So a slug may not equal any other product's slug, ID or SKU, and a SKU may not equal any other product's SKU, slug or ID. A transaction-scoped advisory lock serializes these checks between staff writes. Dictionaries are read-only in B1.

### Collections

A write sets the product's memberships to exactly the given list:

- removed memberships are deleted;
- kept ones keep their `sort_order`;
- new ones go to the end of their collection.

This happens in the same transaction, with one revision bump.

### Media

`image` and `gallery` are returned read-only. New products have none, and the storefront already renders a missing image. There is no upload, path or URL input until Phase C.

### What customers see of an archived product

| Path | Behavior |
| --- | --- |
| Catalog, search, suggestions, facets, category/manufacturer/brand counts, collections | omitted |
| Product detail (by ID, slug or SKU) | 404 |
| Favorites | row kept, hidden; shown again after a restore; can't be newly added (404) |
| Cart | line kept, shown with issue "Товар больше не продаётся" and `available: 0`; can be removed; can't be added or changed (404) |
| Checkout | `409 out_of_stock` when the line is selected; nothing is written |
| Repeat (v1, v2) | `discontinued`; v2 commit refuses it as changed |
| Order history | unchanged (snapshots) |

### Checkout revalidation

Checkout already repriced every line from the locked product row and offered the review guard (`checkout_changed`). That is unchanged. On the same locked rows it now also refuses:

- archived products, with `out_of_stock`;
- quantities that no longer satisfy the current minimum and step, with `checkout_changed`.

The quantity rule is *quantity ≥ minimum and (quantity − minimum) mod step = 0*, the one `SetCartItem` and repeat already applied. Nothing is rounded. The cart reports such lines with an issue, so the storefront's existing issue handling blocks checkout until the customer fixes them. The memory adapter applies the same rule.

### Concurrency and lock order

- **Staff write:** actor `staff_users` row `FOR SHARE` (`InStaffMutation`) → product row `FOR UPDATE` → identifier advisory lock → dictionary key-share locks and `collection_products`.
- **Checkout and cart writers:** `users` row → product rows by ID → `cart_items`.

Neither path takes the other's first lock, so no cycle can form. A staff stock edit racing checkout either commits first, so the sale comes off the new stock, or becomes stale, which returns 409 with no audit event. Tests cover both outcomes.

### Audit

Each successful mutation runs inside `InStaffMutation` and writes exactly one event with target `product`/ID. Validation errors, stale revisions, missing products, conflicts, refused authorization and failed transactions write none. If the audit insert fails, the product change rolls back.

## Rollback

Migration 013 only adds columns. B0 and earlier images run on it, and their migrators treat 013 as a newer version. They register no product routes. But those images don't know `archived_at`:

- **Archived products reappear.** After a rollback below B1, archived products show again in the catalog and can be bought.
- **Sales stop bumping the revision.** In a mixed deployment, older replicas sell without incrementing it, so a staff edit could overwrite a sale processed there.

Operational rule:

- Finish a rollout (or rollback) across all replicas before staff edit products.
- Before rolling back below B1, run `SELECT id, stock FROM products WHERE archived_at IS NOT NULL`. If any listed product must stay off sale, either don't roll back, or deliberately set its stock to 0 first and record the old values. That SQL bypasses the staff audit log.

There is no destructive down migration for production; the down file is for development databases.

## Not in B1

Category, manufacturer, brand, collection and badge management; media; bulk edits and imports; order administration; roles; the admin UI and its public edge host. The shop edge still answers 404 for `/api/admin/*`.
