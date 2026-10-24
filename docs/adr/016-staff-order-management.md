# ADR 016: Staff order management

Status: Accepted for Phase B (slice B4). Date: 2026-10-24. Builds on ADR 012 (audit), ADR 013 (product revisions and stock), ADR 014 and 015 (admin application).

## Context

Until B4 nothing changed an order after checkout. The repository shows:

- **Statuses.** Checkout writes `processing` with the stored label "Собираем заказ"; the demo seed has `delivered` / "Доставлен" with `delivered_at`. The legacy web UI already names two more statuses, `transit` ("В пути") and `cancelled` ("Отменён"). The customer contract types `status` as free text and tells clients to show `statusLabel`, and the Next storefront renders unknown statuses neutrally.
- **Stock.** Checkout takes stock when the order is created (no reservations) and bumps each product's revision (ADR 013).
- **What an order stores.** Lines are snapshots; orders store no payment data, no shipping data and no customer-contact snapshot (only `user_id`, the delivery address and the comment).

## Decision

### Lifecycle

```
processing ──▶ transit ──▶ delivered
     │
     └──▶ cancelled   (ordered stock returns to inventory)
```

`delivered` and `cancelled` are terminal, and so is any status the table does not know.

- The table lives in `internal/domain/order_admin.go`.
- Every change is checked on the server under the order row lock.
- No route assigns an arbitrary status.
- Moving to `delivered` sets `delivered_at`.
- Labels are the existing ones and are stored with the status, as checkout does. Older binaries therefore show staff-set statuses as they are.
- Cancelling an order in transit is not offered: the goods have left the warehouse, and returns are a later decision.

There is no payment, refund, shipping provider or customer notification. The UI says so where it matters.

### Schema: migration `014_order_revisions`

One column: `revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)`. Existing orders and new checkouts start at 1. Every staff change increments it exactly once and must send `expectedRevision`; a mismatch is `409 stale_revision` with nothing written and no audit event.

### API (`/api/admin/v1`, `api/admin-openapi.yaml`)

| Route | Action |
| --- | --- |
| `GET /orders?status=&q=&page=&limit=` | newest first; `q` is the order ID or number (with or without `#`), or a substring of the customer's email or name |
| `GET /orders/{order}` | snapshot lines and totals, current customer account data, `allowedTransitions` |
| `POST /orders/{order}/status {status, expectedRevision}` | `transit` or `delivered` only; audited as `order.status_change` |
| `POST /orders/{order}/cancel {expectedRevision}` | audited as `order.cancel` |

- **Errors.** A transition the current status does not allow is `409 invalid_transition`. Using `cancelled` or `processing` on the status route is `422`. There is no DELETE and no PUT/PATCH (405).
- **Protection.** The same staff session, CSRF, expected-staff and authorization rules as products apply. Reads use `Authorize(…, "order.read")`.
- **Search caveat.** Case-insensitive matching of non-ASCII letters follows the database locale, as product and customer search already do; a C-locale database folds only ASCII.

### Cancellation and stock

Cancellation runs in one `InStaffMutation` transaction:

1. lock the order;
2. check the revision and the transition;
3. lock the ordered products `FOR UPDATE` in ID order;
4. add back each product's ordered quantity and bump the product's revision;
5. update the order and its revision;
6. append the audit event;
7. commit.

Details:

- **At most once.** An order reaches `cancelled` only from `processing`, under the order lock, so its stock returns at most once. A second cancel is `invalid_transition`; a concurrent one is `stale_revision`.
- **Archived products** get their stock back and stay archived.
- **Deleted products.** A product row that no longer exists gets nothing back.
- **Snapshots are never touched.**
- **Product revision.** Restocking bumps it, like a sale, so a staff stock edit based on the earlier value is refused instead of overwriting the returned units (ADR 013).

### Lock order

| Path | Locks, in order |
| --- | --- |
| Staff order change | staff actor row → order → products (ID order) |
| Repeat-order commit | customer row → order `FOR SHARE` → products |
| Checkout | customer row → products (ID order) → cart lines; never locks an existing order |
| Staff product edit | staff row → product → identifier advisory lock |

No path takes a product before an order, or an order after waiting on a product it then needs, so no cycle forms. A real-PostgreSQL test runs cancel, staff stock edit and checkout of the same product concurrently, and none deadlocks.

### Customer side

- **Order history** shows the stored label; lines and totals stay the checkout snapshot. The customer OpenAPI documents the possible statuses (description only).
- **Repeat ordering** does not depend on status and still works for cancelled orders, with the existing unavailable/discontinued handling.
- **No customer endpoint changes.**

### Admin UI

- **Navigation and pages.** "Заказы" is a real link. `/orders` lists orders, keeping status, search, page and size in the URL. `/orders/{id}` shows the snapshot: lines, the goods total, delivery (the difference to the stored total, when there is one), the total, the address and comment, and the customer's current account data, labelled as such.
- **Actions.** Exactly the `allowedTransitions` Go returns, as named buttons ("Передать в доставку", "Отметить доставленным", "Отменить заказ…"). There is no status dropdown. Terminal orders say so in text, with no disabled controls.
- **Cancellation** needs an inline confirmation. It says the stock returns, and that nothing is refunded and no one is notified.
- **Stale revision and invalid transition** both show "Заказ был изменён другим сотрудником…" (or the invalid-transition text). The page keeps the shown order, never retries, and disables actions until the operator explicitly loads the current version.
- **Session and CSRF.** Requests go through `useStaffApi`: in-memory CSRF with one refresh, expected staff ID, `401` to sign-in.

## Rollback

Migration 014 only adds a column with a default.

- **Rolling back to B3 or earlier:** older images verify and migrate past it, still insert orders (getting revision 1), and show `transit` and `cancelled` orders using their stored labels.
- **Restocks are never undone.** If such an image is deployed, cancellations already made stay cancelled with their stock returned.
- **No route to change orders** exists in older images.

## Not in B4

Payments and refunds, shipping integrations, customer notifications, bulk actions, order editing, returns, deletion, roles, MFA, public admin exposure.
