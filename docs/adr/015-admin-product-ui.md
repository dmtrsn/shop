# ADR 015: Admin product management UI

Status: Accepted for Phase B (slice B3). Date: 2026-10-24. Connects the B1 staff product API (ADR 013) to the B2 admin application (ADR 014).

## Decision

### Routes

All product pages live in `apps/admin/src/app/(staff)`, behind the server session gate:

| Route | Content |
| --- | --- |
| `/products` | list |
| `/products/new` | create |
| `/products/{id}` | edit, archive and restore |

The navigation links "Товары"; "Заказы" stays an unlinked "скоро" label.

Pages render dynamically. Product data loads in the browser from `/api/admin/v1/*` on the admin origin, so no product data or CSRF token is rendered or cached by the admin server.

### List

The URL holds the list state: `q`, `status` (active, archived or all), `page`, and `limit` (25, 50 or 100). Reloads, back/forward and shared admin links show the same list. Go does all the searching; there is no client-side index.

The table shows name, SKU and ID, price, stock, category and manufacturer, status as a text badge, and revision. The two lowest-priority columns are hidden below 56rem, and the table scrolls horizontally inside its own box rather than the page.

The page also has loading, empty, past-the-end and error states, plus a retry.

### Form

The form edits exactly the B1 write document. Each field is a string as typed; on submit it becomes the 23-member document.

- **Money.** Prices are entered in roubles (`199`, `199,90` or `199.90`). They are converted to kopecks by string parsing and integer arithmetic only, never by multiplying floats. More than two decimals, signs, exponents, spaces and amounts above the B1 bound are rejected, not rounded.
- **Whole numbers.** Stock, minimum, step, weight and shelf life must be digits only; nothing is clamped.
- **Lists.** Tags and badges are one per line.
- **Media.** Image paths are shown read-only. There is no upload, URL or path input until media management exists.

Client checks only catch typos: required fields, number formats, and price > 0. Go validates every write. Its field errors are mapped to controls (`priceMinor` → price, and so on) and listed in a focusable summary with links to the fields. Server text that names no field appears in the summary.

### Reference data: `GET /api/admin/v1/catalog-options`

B1 offered no way to list dictionaries, and the customer API is not exposed on the admin host. So B3 adds one read-only staff endpoint (`getCatalogOptions` in `api/admin-openapi.yaml`). It returns every category (with its parent), manufacturer, brand (with its manufacturer) and collection, including dictionaries no product uses yet.

It needs the staff session and `Authorize(…, "product.read")`, writes no audit event, and is 404 on the shop host like every `/api/admin` path.

In the form:

- categories are indented under their parents;
- the brand list shows only brands of the chosen manufacturer;
- changing the manufacturer clears a brand that does not belong to it;
- collections are a checklist.

B1 still rechecks every reference on write.

### Optimistic concurrency

The editor keeps the revision it loaded. Every save, archive and restore sends it. On `409 stale_revision` the editor does not:

- retry;
- reload by itself;
- merge;
- touch the form.

Instead it shows "Товар был изменён другим сотрудником. Ваши изменения не сохранены." and disables saving until the operator chooses:

- "Загрузить актуальную версия": replaces the form and revision with the server's;
- or "Отменить мои изменения и вернуться к списку".

The local values stay visible until then, so they can be copied by hand.

### Archive and restore

Archiving needs an inline, keyboard-reachable confirmation. Its text says what B1 does: the product leaves the catalog, search and collections and cannot be bought, while orders, carts and favorites are kept and the product can be restored. Restore needs no confirmation.

Both are unavailable while the form has unsaved changes. After either, the page shows the returned state and revision. There is no delete control and no DELETE request anywhere.

### Unsaved changes

While the form differs from the last server state:

- the browser asks before unload or reload;
- a click on any same-origin link asks first (a capture-phase listener runs before Next's link handler).

Back and forward buttons are not intercepted, because the App Router offers no hook. After a successful save or reload, the form is clean again.

### Session and CSRF

Every request goes through one hook, `useStaffApi`, built on the B2 session provider. It applies this policy:

- **Mutations** carry the in-memory staff CSRF token and `X-Expected-Staff-ID`.
- **CSRF rejection:** a `403 csrf_failed` (rejected before any change) is retried once with a token freshly fetched from Go. Nothing else is retried.
- **Expired session:** `401` moves to `/login` once, and unsaved form state is dropped with the page.
- **Another staff member signed in:** `409 authenticated_staff_changed` re-renders the page from the server.

Nothing is stored in browser storage, and drafts are not persisted.

## Verification

- **Unit and component tests** (Vitest) cover:
  - money parsing;
  - the form model and payload;
  - the request layer's error mapping and headers;
  - list states and URL handling;
  - the editor: create, edit, field errors, the stale-revision flow, the CSRF single retry, expiry, archive/restore and unsaved-changes guards.
- **Integration (real PostgreSQL):** `TestCatalogOptionsForStaffOnly`.
- **Admin contract:** covers `getCatalogOptions`.
- **Browser journey on the real stack:** `scripts/test-admin-product-flow.mjs`, run through `scripts/with-admin-tls-proxy.sh`. It signs in, searches, edits, checks the shop, makes a two-tab stale conflict and reloads, archives (the shop stops selling), restores (sold again) and signs out. It then checks the audit table:
  - exactly one `product.update` per successful save;
  - one `product.archive` and one `product.restore`;
  - none for the stale save;
  - each with the staff actor and a request ID.

## Not in B3

Dictionary, collection and media management; orders; bulk edits; audit browsing; roles; MFA; a public admin deployment.
