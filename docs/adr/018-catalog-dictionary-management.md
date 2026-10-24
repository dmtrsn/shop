# ADR 018: Catalog dictionary management

Status: Accepted for Phase B (slice B6). Date: 2026-10-24. Builds on ADR 012 (audit), ADR 013 (product revisions) and ADR 015 (admin product UI).

## Context

Products refer to four dictionaries:

- `categories`: a tree through `parent_id`. The demo data is two levels deep, and the storefront category filter includes direct subcategories.
- `manufacturers`.
- `brands`: each belongs to one manufacturer. Products pair `(brand_id, manufacturer_id)` through the deferred foreign key from migration 004.
- `collections`: products join them through `collection_products`; `starts_at`/`ends_at` bound when the storefront lists them.

Until B6 only migrations and direct SQL changed these tables. B3's product form read them through `catalog-options`.

Customer filters accept a dictionary ID or its slug. Tags and badges are free-text arrays on the product, not managed entities, so B6 leaves them alone.

## Decision

### Schema: migration `016_catalog_dictionary_management`

Each of the four tables gains:

- `revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)`;
- `archived_at timestamptz`.

Existing rows start active at revision 1. **Nothing is ever hard-deleted**: no route deletes, and archive is the only way to withdraw a row.

### API (`/api/admin/v1`, `api/admin-openapi.yaml` 0.6.0-b6)

Each dictionary — `categories`, `manufacturers`, `brands`, `collections` — has six routes:

| Route | Action |
| --- | --- |
| `GET /{dictionary}?status=active\|archived\|all` | storefront order (sort order, name, ID); default `active` |
| `GET /{dictionary}/{id}` | one entry with revision, archive state and usage counts |
| `POST /{dictionary}` | create; server ID `cat_`/`mfr_`/`brd_`/`col_` + 32 hex digits, revision 1; `<entity>.create` |
| `PUT /{dictionary}/{id}` | full replacement with `expectedRevision`; `<entity>.update` |
| `POST /{dictionary}/{id}/archive` | `{expectedRevision}`; `<entity>.archive` |
| `POST /{dictionary}/{id}/restore` | `{expectedRevision}`; `<entity>.restore` |

**Request bodies.** Every member is required and unknown members are rejected; collection `startsAt`/`endsAt` may be `null`. Logo and image paths are read-only (there is no media management).

**Protection.** The staff session, CSRF, expected-staff and authorization rules are those of products. Reads are authorized as `dictionary.read`.

**Revisions and audit.**

- A wrong revision is `409 stale_revision`, with no write and no audit event.
- Archiving an archived row, or restoring an active one, is `409 conflict`.
- Every success increments the revision once and commits one `InStaffMutation` audit event (entity type `category`, `manufacturer`, `brand` or `collection`).
- Audit failure rolls the change back.

**Slugs** are operator-controlled: a–z, 0–9 and inner hyphens. Because customer filters accept either, a slug may equal no other slug or ID **in the same dictionary**.

New error codes:

- `422 invalid_hierarchy`;
- `409 reference_conflict`, with `error.references: {products, brands, categories}`.

Missing or archived references are `422 validation_error` on the field.

### Category tree

The tree is two levels deep, enforced on the server:

- `parentId` is empty or names a top-level category;
- a category is never its own parent;
- a category with any subcategory, archived or not, cannot get a parent.

The last rule means restoring a subcategory can never create a third level. With two levels, cycles are impossible.

### Archive and restore

| Dictionary | Archive refused (`reference_conflict`) while | Restore requires |
| --- | --- | --- |
| Category | any product (archived or not) uses it, or it has an active subcategory | an active parent |
| Manufacturer | any product uses it, or it has an active brand | — |
| Brand | any product uses it | an active manufacturer |
| Collection | never refused | — |

**Products count even when archived**, since an archived product can be restored.

**An archived collection keeps every membership.** It leaves the storefront collection list and the collection filter until restored, and products in it stay editable.

**Brand moves.** A brand used by any product keeps its manufacturer: changing it is `reference_conflict`, because products pair the two.

### Product writes (B1, amended)

`checkProductWrite` now:

1. takes the referenced category, manufacturer and brand rows `FOR SHARE`, then the requested collections in ID order;
2. requires every **new or changed** reference to be active ("Категория в архиве", and so on).

A reference the product already has stays usable even if it was archived meanwhile, including archived collection memberships. An editor saving an unrelated change never has to repair references first.

`catalog-options` now carries `archived` for every entry. The product form:

- offers an archived entry only where the loaded product already uses it, labelled "(в архиве)";
- never offers one on a new product.

### Concurrency and lock order

All dictionary writes take one transaction-scoped advisory lock (`sweetshop/catalog-dictionaries`) right after the actor's staff row. Tree, slug and reference checks therefore never race another dictionary write.

| Path | Locks, in order |
| --- | --- |
| Dictionary write | staff row → dictionary advisory lock → the row `FOR NO KEY UPDATE` → referenced parent or manufacturer `FOR SHARE` |
| Product write | staff row → product `FOR UPDATE` → product-identifier advisory lock → category, manufacturer, brand, collections `FOR SHARE` |
| Checkout, order cancel | customer or staff row → products; never a dictionary row |

The row lock of an archive or brand move conflicts with a product write's share lock:

- **Archive or move commits first:** the product write then sees the row archived (or the brand's new manufacturer) and is refused.
- **Product write commits first:** the archive or move then counts its reference and is refused.

No path takes a dictionary row before a product it then waits for, so no cycle forms.

Real-PostgreSQL tests race:

- archive against assignment;
- brand move against assignment;
- two tree edits that would build a third level;
- eight concurrent edits of one row.

### Customer side

Customer reads skip archived rows:

- category, manufacturer, brand and collection lists;
- the category and collection filters;
- search suggestions.

Products never point at an archived category, manufacturer or brand (archive is refused while any product does), so catalog, product and facet reads need no further change.

Order lines keep their checkout snapshot. Renaming a manufacturer does not touch them.

### Admin UI

**Section.** "Справочники" in the navigation, with tabs for the four dictionaries:

- `/dictionaries/{dictionary}`: list with the status filter in the URL, categories shown as their tree;
- `/dictionaries/{dictionary}/new` and `/dictionaries/{dictionary}/{id}`: the editor.

**Shared plumbing.** The list and editor share the revision, conflict and archive handling. Each dictionary keeps its own fields and types:

- the category parent select offers active top-level categories only, and is locked for a category with subcategories;
- the brand's manufacturer select is locked while products use the brand;
- collection dates are whole Moscow days.

**Conflicts.** A stale save shows "Запись была изменена другим сотрудником…" and keeps the operator's values. Nothing is retried; the operator explicitly loads the current version.

**Archive refusals.** A refused archive names the counts ("товаров — N, активных брендов — M"). Archive needs an inline confirmation that explains the consequence.

**Session.** Requests go through `useStaffApi`, as everywhere else.

## Rollback

Migration 016 only adds columns with defaults. B5 and older images verify and migrate past it (tested), and their dictionary inserts still work.

**Hazard: older binaries ignore `archived_at`.** After a rollback, the storefront shows archived dictionary rows again, and older product writes may assign them.

Before rolling back to a pre-B6 image, either accept that or restore the rows that must stay hidden. The columns keep their values, and the archive state applies again on roll-forward.

The down migration drops the columns and is for development databases only.

## Not in B6

Media upload or deletion, managed tags or badges, deeper category trees, bulk operations, deletion, product redesign, order changes, roles, MFA, public admin exposure.
