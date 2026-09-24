# ADR 005: Preserve transactional purchasing and explicit repeat review

Status: Proposed. Date: 2026-09-25. Depends on ADRs 001 and 002.

## Context

Existing repeat preview uses order snapshots, batched product reads, current cart quantities and minimum/step arithmetic. Commit rejects invalid explicit selections and performs a repository batch operation. PostgreSQL locks and validates all repeated products before adding. Checkout locks selected product/cart rows, writes order snapshots, decrements stock and removes only captured lines atomically. These are valuable mechanisms to retain.

Gaps are consent/version/idempotency contracts, repeated full-list payloads, incomplete review presentation, and business rules duplicated between adapters/browser. This ADR extends the workflows rather than replacing them.

## Decision and invariants

1. Preview never mutates the cart. Opening or refreshing repeat review never commits.
2. Repeat quantities are additions. Final cart quantity must satisfy minimum and step: `final >= minimum && (final - minimum) % step == 0`; validate against current stock while locked.
3. All chosen additions succeed atomically or none do. Never silently reduce quantities, select unavailable lines, accept new prices or substitute products on commit.
4. Checkout uses only selected eligible lines and current authoritative prices. Cart presence does not reserve inventory. A reservation/payment workflow is a later explicit extension.
5. Orders preserve purchased names, SKU, unit prices, totals and fulfillment facts independently of live catalog edits or archival.
6. Persisted server checkout selection is distinct from temporary UI selection for a bulk action. Filtering/collapsing does not alter either implicitly.

## Repeat-order extension

Retain `RepeatPreview`, `CommitRepeat`, the addition arithmetic and atomic repository boundary. Add a versioned strict contract first, e.g. `POST /api/v2/orders/:id/repeat/preview` and `/commit`. Preview returns a short-lived, user/order-bound review ID with cart revision, relevant product price/rule revisions, original/current prices, stock states and suggested valid additions. Persist a bounded review snapshot, or an authenticated equivalent; avoid trusting browser-provided prices alone.

Commit requires review ID, explicit nonempty selections and an idempotency key. Missing/empty selections return validation failure, never default additions. Validate ownership, expiry and duplicate/foreign product IDs. Revalidate under the same transaction as cart addition. If cart/price/rules changed, return `409 review_changed` with enough data to refresh review and preserve the customer's edits; require another explicit confirmation. Stock reductions that invalidate selected quantities return line issues. Increased stock does not silently increase quantities. No cart writes on any conflict.

Store the idempotency record and mutation result atomically, scoped by user and operation with a request digest. Reusing a key with different content is a conflict; replaying an identical confirmed request returns its original result without another addition. Check this record before rejecting a consumed review or stale cart revision so retries after successful commit remain recoverable. Define expiry/retention and concurrent-in-flight handling.

The existing UI already sends explicit item arrays, but existing direct clients can send `{}`. Migrate the legacy repeat client to the strict contract before public production and then reject/deprecate the empty-default behavior deliberately. During compatibility, retain the old API only as a documented transition exception; do not claim it enforces mandatory review. No rewrite of the quantity algorithm is required.

Use a full-page review for large orders, with a modal as an optional compact presentation. Show deselection, editable additions, requested/available/already-in-cart/final quantities, unavailable/discontinued states and old/new unit prices with deltas. Include current rules and inline errors. Stable focus, problem filters, search, grouping, compact mode and a sticky review total support long lists. Preserve edits on recoverable conflicts. Displaying suggested replacement data is allowed; selecting a replacement requires a new explicit validated selection model tied to the original line. Current commit rejects arbitrary replacements, so do not wire an unrestricted add button into it.

## Cart and favorites read/write contracts

Add a `carts` aggregate row (initially one per user) with a monotonic revision. Every mutation, including selection/deletion/checkout, changes it in the same transaction. Existing `Cart.updatedAt` is not a revision. Introduce ETag/`If-Match` or explicit expected revision in the new contract; return structured stale-version conflicts. Coordinate all writes with a documented consistent lock order, such as cart aggregate then sorted products then cart rows; verify it across checkout, repeat, inventory and multi-cart operations.

Keep legacy full responses. New read endpoints return compact lines, group metadata and authoritative overall/selected/filtered summaries, with bounded pagination and stable ordering. Group membership refers to line IDs rather than duplicating full products. A cart aggregate revision covers cart edits; live stock and price also need their own revisions or fresh offer checks because another customer's checkout does not update this cart's revision.

New batch commands support setting quantities, selecting explicit scopes, removal, adding favorites to cart and saving cart lines to favorites. For bounded interactive commands, default to atomic validation and structured per-line errors. If an action intentionally allows partial success, label it explicitly and return per-line outcomes. Avoid one HTTP request per product. Bound operation sizes; large imports use jobs and review (ADR 006).

Bulk scope is explicit: IDs, group, visible page, all filtered matches, or entire list. A query-wide operation is tied to a revision/filter snapshot with exclusions and affected count; an empty ID array never means all in the new API. Summaries distinguish positions and units. Summary checkout state is independent of current search results. Favorites gain list revision, search/filter/pagination, batch add/remove and reviewable add-to-cart results. UI preferences remain local; they do not require database columns.

## Checkout and inventory evolution

Introduce an authoritative checkout review/quote tied to cart and price revisions; commit requires expected totals/currency or quote ID plus idempotency. On changes, return updated review rather than placing an order at an unseen price. Revalidate stock, product status, minimum and step in the transaction. Preserve row locking, batch inserts/decrements and exact purchased-line deletion.

Add immutable order currency and subtotal/discount/delivery/total breakdown, structured recipient/contact/address and delivery selection. Historical fields absent from old orders must remain explicitly unknown; do not fabricate historical fees from today's delivery rule. The present payment radio buttons only write a comment. Payment processing, reservation expiry, cancellation/refund state transitions and provider webhooks need a separate design before real charging.

Initially retain `products.stock` as the authoritative stock counter. Add immutable inventory movements for checkout and staff adjustments, written in the same transaction, with reason/actor/reference. Model opening balances for existing stock explicitly. Do not introduce a second authoritative balance during rollout. Introduce lots/expiry/warehouses/reservations only with confirmed fulfillment requirements. Archive products instead of deleting cart/favorite references; render discontinued lines with actionable issues.

## Proposed API/database additions

| Area | Additions, preserving legacy contracts |
| --- | --- |
| Cart | `/api/v2/cart` compact read, `/items` paged read, `/batch` mutation with expected revision; `carts` revision row |
| Favorites | Paged/searchable compact read and explicit batch operations; aggregate revision |
| Repeat | Strict preview/commit DTOs; expiring review snapshots and idempotency records |
| Orders | Paged history summaries, detail on demand; quote/commit contract; immutable financial/contact fields |
| Inventory | Movement ledger, publication/archival state, product price/rule revisions |

## Alternatives, consequences and acceptance

Rejected: automatically replaying a past order into cart, creating a parallel repeat service, client-only stock checks, and one-request-per-line transactions. Extra reviews and revisions add conflict handling, but make customer intent and retries reliable.

Require real PostgreSQL tests for all-or-nothing repeat, duplicate commits, two tabs, simultaneous checkout/stock adjustment, changed quantity rules, stale price, foreign-order access, deleted/archived products, empty selection and review expiry. Assert preview leaves cart unchanged; no oversell, duplicate order or unrelated-line loss occurs. Preserve current regression tests, including deselection of unavailable lines and final quantity minimum/step tests. Exercise mobile/keyboard workflows with 100/500/1,000 lines and verify totals do not depend on pagination or collapse state.
