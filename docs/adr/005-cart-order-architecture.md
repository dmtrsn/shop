# ADR 005: Preserve transactional purchasing and explicit repeat review

Status: Strict Repeat v2 accepted/implemented 2026-10-04; other extensions remain proposed. Original date: 2026-09-25. Depends on ADRs 001 and 002.

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

Checkpoint B (2026-10-04) implements `POST /api/v2/orders/{order}/repeat/preview` and `/commit`. Preview is a stateless read: no stored review, review ID, TTL, reservation or idempotency record. Each line exposes its actual historical `sourcePosition`, historical snapshot, current original Product when present, existing Cart quantity/selection, availability and suggested additions. Replacement Product information remains informational only. Suggestions describe each line independently; duplicate-product selections are validated together at commit.

Commit requires exactly one `Idempotency-Key` and explicit nonempty `items`. Each item carries `sourcePosition`, canonical original `productId`, units **to add**, and reviewed expectations for existing Cart quantity, unit price, currency, minimum and step. Missing/null/empty items, duplicate positions and malformed expectations return `422 validation_error`. Historical source/product mismatch, deletion or changed expectations return `409 repeat_changed`; insufficient current stock returns `409 out_of_stock`. Aggregated final quantities must satisfy stock, minimum and step. Invalid minimum/step quantities return 422. No default additions, replacements, clamping, stock reservation or automatic retry.

A server-computed versioned SHA-256 digest (`repeat/v2`) binds the canonical owned order ID and every explicit item/expectation, sorted by source position. Migration `007_repeat_commits` stores successful receipts only, with a unique `(user_id, idempotency_key)` and non-null validated key/digest. Receipt additions and Cart increments commit atomically. A same-key/same-digest retry returns the original receipt plus the **current authoritative Cart**, with `200` and `Idempotent-Replayed: true`; a different digest returns `409 idempotency_key_reused`. Receipt lookup precedes current review validation. A later Cart edit does not invalidate recovery, and Cart GET alone cannot prove operation success. There is no receipt expiration in this slice.

Every PostgreSQL Cart-mutating transaction first locks the authenticated user's existing `users` row with `FOR NO KEY UPDATE`, held through commit/rollback. This mode serializes Cart writers for that user while remaining compatible with foreign-key `KEY SHARE` locks from sessions/favorites/orders; `FOR UPDATE` would introduce unnecessary conflicts with those readers. No runtime repository flow updates/deletes users or takes a conflicting user lock after product/Cart locks. No new lock table, migration, hash key or Cart revision is required. Different users remain concurrent except on shared products.

The static lock order is user row → (Repeat v2 historical shared locks) → canonical products sorted by ID → affected Cart rows sorted by product ID → writes. `SetCartItem`, `RemoveCartItem`, `ClearCart`, `SetCartSelection`, `AddCartItems` (legacy Repeat), `createOrder`, and `commitRepeatV2` all participate. Remove/clear/selection skip product row locks and lock affected Cart rows in order. Cart GET remains read-only. Foreign-key checks on already held products/user rows add no reverse dependency; session/favorite user-FK locks are compatible with the chosen user lock. Migrations, direct SQL and future administrative deletion/writer paths must be audited separately before being used concurrently with purchasing.

Checkout locks selected products first, then reads the complete selected Cart without an earlier-ID filter. An addition/selection committed before its user lock is acquired is visible to the exact expectation guard and returns `checkout_changed` with no Checkout writes. Once Checkout owns the lock, other same-user Cart writers wait through commit or rollback. This replaces the insufficient captured-product filter and closes the later-in-transaction membership race too.

Repeat v2 uses the same user lock; its separate key advisory lock was removed. Committed receipts can replay before taking the Cart lock. After acquiring it, a mutating attempt rereads the receipt before any review validation, so concurrent same-key requests replay or reject a different digest without another increment. The unique receipt constraint remains the durable guarantee; only its named violation is interpreted as a receipt race. Checkout's existing committed-key fast path is unchanged. Memory retains its existing mutex.


Legacy v1 `RepeatPreview`/`CommitRepeat`, including missing/null/empty-item defaults, selected flags, addition semantics and informational replacement behavior, remain unchanged. No legacy client migration is part of Checkpoint B. The Next review/commit UI was delivered in the subsequent storefront checkpoint.

Use a full-page review for large orders, with a modal as an optional compact presentation. Show deselection, editable additions, requested/available/already-in-cart/final quantities, unavailable/discontinued states and old/new unit prices with deltas. Include current rules and inline errors. Stable focus, problem filters, search, grouping, compact mode and a sticky review total support long lists. Preserve edits on recoverable conflicts. Displaying suggested replacement data is allowed; selecting a replacement requires a new explicit validated selection model tied to the original line. Current commit rejects arbitrary replacements, so do not wire an unrestricted add button into it.

## Cart and favorites read/write contracts

Future proposal, outside strict Repeat v2: add a `carts` aggregate row (initially one per user) with a monotonic revision. Every mutation, including selection/deletion/checkout, changes it in the same transaction. Existing `Cart.updatedAt` is not a revision. Introduce ETag/`If-Match` or explicit expected revision in the new contract; return structured stale-version conflicts. Coordinate all writes with a documented consistent lock order, such as cart aggregate then sorted products then cart rows; verify it across checkout, repeat, inventory and multi-cart operations.

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
| Repeat | Implemented stateless v2 preview, explicit reviewed expectations and durable commit receipts |
| Orders | Paged history summaries, detail on demand; quote/commit contract; immutable financial/contact fields |
| Inventory | Movement ledger, publication/archival state, product price/rule revisions |

## Alternatives, consequences and acceptance

Rejected: automatically replaying a past order into cart, creating a parallel repeat service, client-only stock checks, and one-request-per-line transactions. Extra reviews and revisions add conflict handling, but make customer intent and retries reliable.

Require real PostgreSQL tests for all-or-nothing repeat, duplicate commits, two tabs, simultaneous checkout/stock adjustment, changed quantity rules, stale price, foreign-order access, deleted/archived products, empty selection and replay after later Cart edits. Assert preview leaves cart unchanged; no oversell, duplicate order or unrelated-line loss occurs. Preserve current regression tests, including deselection of unavailable lines and final quantity minimum/step tests. Exercise mobile/keyboard workflows with 100/500/1,000 lines and verify totals do not depend on pagination or collapse state.
