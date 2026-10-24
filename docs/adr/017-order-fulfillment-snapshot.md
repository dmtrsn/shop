# ADR 017: Order-time customer contact

Status: Accepted for Phase B (slice B5). Date: 2026-10-24. Corrects the B4 staff order view (ADR 016).

## Context

An order must keep whom it was placed for. The B4 staff view read the customer's name, email and phone from the `users` row at the moment staff opened the order. That is today's account, not the order's: once an account changes, every old order would silently show the new details.

What checkout actually has:

| Value | Source | Stored on the order before B5 |
| --- | --- | --- |
| Delivery address | checkout request `deliveryAddress` (trimmed, required) | yes, `orders.delivery_address` |
| Comment | checkout request `comment` | yes, `orders.comment` |
| Lines, prices, totals | locked product rows | yes, `order_items` and `orders.total_minor` |
| Customer name, email, phone | the customer's `users` row | **no** |

There is no fulfillment method, address book, structured address, or per-order recipient in the checkout request. The application offers no way to change a customer's name, email or phone after registration or operator provisioning, where they are validated and normalized. Direct SQL, or a future profile feature, can still change them.

## Decision

### Columns: migration `015_order_fulfillment_snapshot`

`orders.customer_name`, `orders.customer_email` and `orders.customer_phone` are plain text. They are explicit typed columns rather than a JSON blob: only what fulfillment needs, renderable, and with a clear retention surface.

- A check constraint makes them all-or-nothing.
- NULL means "not recorded", never "empty".
- An empty phone is `''`, meaning the customer gave none, as on the account.
- There is no foreign key and no dependency on `users`.

Nothing else is copied: no password hash, session, token, IP address, user agent, staff data or account flags.

### Source and atomicity

`createOrder` already starts by locking the customer's `users` row `FOR NO KEY UPDATE` (`beginCartMutation`). In the same transaction it reads name, email and phone from that row and inserts them with the order. They commit or roll back together with the order, its lines, the stock decrement and the cart cleanup. Because the row is locked, a concurrent account change is ordered strictly before or after the checkout, so the three values are one coherent view. A real-PostgreSQL race test asserts this.

The values are stored exactly as the account holds them; they are not normalized again on write or read. Delivery address and comment were already order-time values and stay as they are. The memory adapter records the contact the same way, under its lock.

### Immutability

Nothing updates the columns after the insert:

- staff status changes and cancellation (ADR 016) touch only status, label, revision, `delivered_at` and stock;
- no customer or staff API accepts them;
- there is no order-edit operation.

Correcting a recipient would be a future, separately designed and audited feature.

Audit rows still hold only actor, action, target and request ID. They never copy order contents.

### Orders without a recorded contact

Orders created before B5, and any order an older replica writes during a rolling deploy, have NULL columns. They are **not backfilled** from today's accounts: that would forge history.

The staff API therefore distinguishes:

- `contact`: the order's own name, email and phone, when recorded;
- `currentAccount`: only when `contact` is absent, the account as it is now, explicitly named as current data and never presented as the order's;
- `customerId`: always, for the account association.

The admin page shows `contact` under "Получатель" with "Как при оформлении заказа." Without one, it says "Контактные данные на момент заказа не сохранены…" and shows the current account in a separate, dashed box titled "Текущие данные аккаунта", captioned "могут отличаться от данных на момент заказа", with no mail link. The list labels such rows the same way.

Search matches either the recorded contact or the current account, because finding orders isn't presenting history.

### Customer side

The customer order API is unchanged. It already showed the order's address and comment, and doesn't need to echo the customer's own contact details. Repeat ordering only fills the cart; the next checkout records the contact the account has then, together with that checkout's own address.

### Privacy and retention

Orders now carry personal data (name, email, phone, besides the address they already held) for as long as the order exists. That is the point, and it makes orders a personal-data record in their own right: account changes do not erase or update them. B5 adds no deletion, retention or anonymization rule. These are later policy and legal decisions, and any such tool must be explicit and audited.

## Rollout and rollback

The migration is additive and nullable, so older binaries keep working on the new schema (tested: the B4 migrator verifies and migrates past 015, and the B4 order insert statement still succeeds, leaving the columns NULL).

- **Mixed deployment.** While B4 and B5 replicas run side by side, orders placed on a B4 replica get no contact, and staff see them as not recorded. Finish the rollout to every application replica before relying on contacts being complete. A NOT NULL constraint can be added by a later migration once every writer is B5 or newer.
- **Rollback to B4.** The B4 admin view shows the current account for every order again, as it did, and orders created meanwhile get no contact. The recorded contacts stay in the columns and reappear on roll-forward.

The down migration drops the columns and is for development databases only.

## Verification

- **Real PostgreSQL:**
  - the A→B history case: order 1 keeps A, the account shows B, order 2 records B;
  - status changes and cancellation leave the contact unchanged;
  - audit rows carry no order contents;
  - a repeat-then-checkout records a fresh contact;
  - legacy and older-replica orders: no backfill, `currentAccount` only, half a contact rejected;
  - a disabled account's orders keep their contact;
  - no credential columns or values in the view;
  - concurrent account change versus checkout gives a coherent contact;
  - the B4 binary tolerates the schema.
- **Memory adapter:** parity test.
- **Admin UI tests** pin the recorded / not-recorded / current distinction.
- **Real-stack browser journey** (`scripts/test-admin-order-flow.mjs`) runs the A→B case through the admin UI, including the seeded pre-B5 order.
