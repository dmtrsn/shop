# Production Baseline and Contracts

Implemented 2026-09-25. Scope: the existing Go/PostgreSQL application and legacy frontend. This records observed behavior and regression checks, not a production certification. The audit, migration plan and ADR direction remain in force; no future frontend or API has been introduced.

## Current HTTP contract

[`api/openapi.yaml`](../api/openapi.yaml) describes all 25 registered method/path operations: readiness, login/current user, categories, manufacturers, brands, collections, catalog, facets, product lookup, suggestions, cart mutations/selection, favorites, history/details, checkout and repeat preview/commit. Go also supports HEAD for GET routes. No `/api/v2` routes exist.

The shared [contract suite](../tests/contract/http.go) validates the OpenAPI document, compares its route inventory with server registrations, exercises successful requests for every operation and validates their responses. It runs against both memory and PostgreSQL. Negative cases cover authentication, missing resources, stock/conflict errors, invalid inputs, unknown JSON members, multiple JSON values, empty bodies and the 1 MiB decoder limit. A pinned Go validator (`github.com/getkin/kin-openapi v0.127.0`, with checksums in go.sum) avoids introducing a frontend build or separate validation service. The document is maintained independently of implementation; changing an HTTP field, envelope or status must pass these tests and deliberate contract review.

Compatibility details captured in the document and tests:

- Prices are integer minor units, including the old `minPrice`/`maxPrice` aliases. `size` aliases `limit`, and `new` aliases `newOnly`; a nonempty canonical parameter wins. `priceAsc`/`priceDesc` are frontend-only spellings, not HTTP sort aliases.
- Catalog defaults to page 1 and limit 24, caps limit at 100, normalizes nonpositive values, and rejects pages above 1,000,000. An empty result has `items: []` and `totalPages: 0`. Facets parse pagination/sort inputs but ignore their values; all filters apply to each facet, including its own axis.
- Product lookup and individual cart/favorite operations accept IDs, slugs and case-insensitive SKUs. PostgreSQL bulk cart selection accepts canonical IDs only. Empty/missing/null selection IDs mean all lines; unknown nonempty IDs change nothing.
- Cart POST assigns an absolute quantity and returns 201 even for an existing line. Quantity validation uses `quantity >= minimum` and `(quantity-minimum)%step == 0`. Selection-only PATCH bypasses stock validation so unavailable lines can be deselected.
- `selectedCount` counts units, including selected unavailable lines. Summary subtotal uses list prices; group subtotal uses current prices. Monetary totals exclude lines with stock issues. Delivery is 29,000 minor units for a positive net subtotal below 350,000, otherwise zero.
- Missing or malformed `selectedOnly` parses false: DELETE clears the entire cart. This dangerous legacy behavior is characterized, not changed.
- Decoded JSON requests have a 1,048,576-byte cap. Invalid, empty, oversized or unknown-field bodies return **422**, not 400/413. Preview ignores its body, reading at most 1,024 bytes without validation. Content-Type is not enforced. Go's case-insensitive member matching and null-to-zero behavior are noted separately from canonical request schemas.
- Empty cart/favorites/facet arrays serialize as arrays. Optional empty scalar/slice fields disappear. `time.Time` fields still serialize zero timestamps despite `omitempty`. PostgreSQL history may emit `items: null` for a manually created order with no item rows; detail emits `[]` for that case. Normal checkout always creates items.
- Login issues a 30-day opaque bearer token, stores only its hash, and sets `no-store`; protected responses also use `no-store`. No logout, revocation, cookie authentication or CSRF protocol was added.
- Readiness is `/api/health`: PostgreSQL ping failure returns 503 with the health envelope. Explicit memory mode returns ready without persistence. There is no separate liveness route.

## PostgreSQL isolation and commands

```sh
make check              # gofmt, vet, unit + memory HTTP/OpenAPI contract, JS syntax
make test-race          # same ordinary Go tests under the race detector
make check-api          # document + complete memory HTTP contract, uncached
make test-integration   # PostgreSQL suite, race detector, uncached
make build
```

`make test-integration` never reads `.env` or `DATABASE_URL`. The [runner](../scripts/test-integration.sh) creates a private temporary PostgreSQL cluster under `/tmp` when local `initdb`/`pg_ctl` are available. It listens on a Unix socket inside that private directory, with no TCP listener, and is stopped/deleted on exit. `PG_BINDIR` selects the local binaries. Otherwise it starts an automatically removed `postgres:16-alpine` Docker container on a random loopback port, with no persistent named volume. To force this Docker path, set `PG_BINDIR=/nonexistent`.

Each top-level test gets a randomly named database created from `template0`, applies the actual migrations where needed, and drops only that database during cleanup. Tests use UTC sessions, bounded contexts and independent fixtures. They do not depend on a developer database or test execution order. No sleep is used to assume transaction ordering: concurrency tests poll `pg_blocking_pids` until the expected transactions really block. Startup readiness polling is separate from concurrency coordination.

CI supplies its disposable PostgreSQL service via `TEST_DATABASE_URL`. For this override the helper requires user `shop_test_admin` and the `postgres` maintenance database, with permission to create/drop databases and install the migration's `pg_trgm` extension. Supply this override only for a disposable test cluster. Integration-tag tests fail, rather than skip, when configuration is missing. Ordinary `go test ./...` intentionally does not run PostgreSQL tests; CI explicitly runs both suites.

The [workflow](../.github/workflows/check.yml) runs formatting, vet, unit/contract tests, Go race tests, PostgreSQL integration/migration/concurrency tests, integration vet, JavaScript syntax, dependency checksum verification, a production build, Compose configuration and whitespace checks. It retains the repository's Go 1.22 baseline and uses PostgreSQL 16 and Node 22. Toolchain support and dependency vulnerability assessment remain a release task; version selection here is not a support/security claim.

## Database and purchasing evidence

| Area | Checks |
| --- | --- |
| Migration application | Empty database; all four versions in filename order; replay preserves modified live data; ignores down scripts; failure rolls back DDL/data and does not record its version; retry succeeds; edited applied SQL is skipped; missing directory fails |
| Constraints | Nonnegative stock/prices; positive minimum/step/cart quantity; cart/favorite uniqueness; case-insensitive email/SKU uniqueness; product/order FKs; deferred brand/manufacturer ownership; no default brand; important indexes |
| Fixtures | 1 user, 10 categories, 8 manufacturers, 12 stored brands (11 public), 24 products, 4 collections, 18 memberships, 4 cart lines, 5 favorites, 1 historical order, 4 snapshot items |
| Identity | Login/email normalization, hashed session storage, authentication, invalid tokens/password, expired session |
| Catalog | Real joins/scanning; lookup aliases; category descendants; manufacturer/brand/series/collection/tags/availability/sale/new/price/search filters; pagination and sort execution; facets; suggestions; collection schedule discrepancy |
| Cart/favorites | Empty arrays; absolute quantity assignment; stock validation; selection scope; canonical-only PostgreSQL bulk IDs; selected deletion; lookup aliases; favorite idempotence; user isolation |
| Checkout | Selected lines only; current prices and delivery; exact historical snapshot fields; stock decrements; purchased lines removed; unselected/unavailable lines remain; insufficient stock leaves order/stock/cart unchanged; no selected lines conflict |
| Transaction rollback | Injected stock-write trigger failure after order/snapshot insertion leaves no partial order, stock change or cart deletion |
| History | Product name/SKU/image/price and manufacturer-name edits do not change snapshots; deleting a live product preserves order items; foreign user cannot read the order |
| Repeat | Preview read-only; all four availability statuses; replacement metadata; price delta/counters; explicit subset/deselection; unavailable/discontinued/foreign/duplicate rejection; final-cart minimum/step rules; legacy empty defaults; all-false no-op; batch failure leaves cart unchanged; no stock reservation |
| HTTP | Same 25-operation request/response contract and negative cases against both adapters |

Concurrency scenarios exercise real repository transactions and locks:

1. Two customers each request two units from three remaining: exactly one checkout succeeds, stock becomes one, one order exists, and the losing cart remains intact.
2. Two concurrent checkouts for the same cart: one succeeds; the other returns conflict after the purchased lines disappear. No duplicate order or second stock decrement.
3. A stock update is uncommitted while repeat preview reads the old stock. Repeat then blocks on the product; after stock drops to zero, locked revalidation fails and every proposed addition is rolled back.
4. Repeat preview completes before another customer's checkout consumes the product. Both commits are forced into observed PostgreSQL contention, checkout proceeds first, and repeat fails without partially adding its other product. A test-only repository wrapper coordinates the boundary between service validation and the real repository; production code has no test hooks.

No oversell, duplicate checkout or partial purchase was reproduced in these coordinated scenarios. These bounded cases do not prove absence of all races, starvation or deadlocks under arbitrary workloads. Returned cart responses are read after transaction commit and need not represent a stable snapshot under further concurrent mutation.

## Migration runner review

The existing runner remains unchanged because single-instance empty replay and failure/retry tests passed. Migrations `001`–`004`, including their down scripts, are unchanged.

The runner creates `schema_migrations`, reads `*.up.sql`, sorts lexicographically, and uses the filename without `.up.sql` as the version. Each script plus its version record executes inside one transaction using the simple query protocol. Previously applied files are skipped. On a failed batch, pgx pool release discards a connection still in a transaction; tests verify rollback and subsequent retry through the pool. There is no transaction around the entire migration set: earlier successful versions remain applied.

Current limitations:

- No deployment serialization/advisory lock, checksums, dirty-state protocol or detection of edited/missing historical files. Tests explicitly demonstrate filename-only skipping.
- Startup runs migrations unless `RUN_MIGRATIONS=false` exactly. Concurrent migration instances remain unsupported; run one instance/release step and disable startup migrations elsewhere.
- Every script must be transactional. `CREATE INDEX CONCURRENTLY` and other nontransactional operations are unsupported. There is no timeout policy beyond caller context, down-runner, dry run or release CLI.
- An empty migration directory succeeds; a missing directory fails after potentially creating the history table.
- Migration `002` always seeds the demo account and catalog, even with production mode. Seed separation requires a reviewed forward/history strategy later; skipping or rewriting `002` would misrepresent history.
- Down scripts are destructive and are not an application rollback. `002` removes orders made by the demo account. Sequence values may have gaps after failed transactions; tests do not promise contiguous order numbers.

## Findings and deliberately preserved differences

The first run had one **test expectation error**: the chocolate parent contains six seeded products, not seven. Inspection of the fixture confirmed the correction; no implementation was changed to pass that test. No new purchasing invariant violation was discovered by the implemented concurrency cases.

The following existing gaps were confirmed or characterized and left unchanged:

| Difference/gap | Evidence and treatment |
| --- | --- |
| Demo fixture drift | Memory is explicitly an independent demo: one fewer stored brand, an extra historical order, different product dates/reviews/descriptions. PostgreSQL fixture totals are asserted independently. These fixture differences are not production bugs to normalize here. |
| Bulk selection aliases | Memory resolves SKU/slug in `productIds`; PostgreSQL matches canonical IDs only. This is an unintentional contract discrepancy, now reported and tested; no adapter semantics changed. |
| Search/tags/sort | Memory uses substring search, case-insensitive tags and stable fixture ordering; PostgreSQL combines full text/LIKE, case-sensitive tag containment and SQL-specific tie-breakers. These implementation differences are retained, not promised as equivalent discovery results. |
| Duplicate batch additions | PostgreSQL rejects duplicate product IDs; memory's repository method does not reject them itself. The service rejects duplicates before reaching either adapter. No HTTP semantics changed. |
| Collection visibility | PostgreSQL listing honors dates; direct catalog filtering ignores them. Memory listing has no schedule filtering. The public fixture has no schedule boundaries. |
| Mutable rules | Cart set validates minimum/step before acquiring the repository lock; checkout rechecks stock, not minimum/step. No product-write API exists, but imports/manual changes can expose this gap. |
| Consent/retries | Repeat default selection, lack of review binding/expected price/idempotency, and checkout lack of idempotency remain. A cart is not a stock reservation. |
| Read consistency | Count/items, facet aggregations and cart product loads use separate queries. Sorts lack unique ID tie-breakers. updatedAt is not a cart revision. |
| Empty historical order | PostgreSQL list/detail differ (`null` versus `[]`) when the database contains an order with no item rows. Normal checkout writes snapshots transactionally; the schema itself does not enforce a nonempty item set. |

## Runtime safety changes

- Unset mode now means `APP_MODE=production`. Production requires a nonblank DSN. Only explicit `development` or `demo` permits memory when the DSN is blank; an invalid mode or invalid/unreachable supplied DSN fails startup. `make run-memory` opts into demo explicitly.
- Malformed DSN errors no longer echo the supplied DSN, which could contain credentials. `make run-postgres` no longer echoes its DSN-bearing command.
- `.env` is removed from the Git index, retained locally and ignored; `.env.example` uses public, local-only example values. Compose now requires supplied database credential/DSN variables rather than embedding credential defaults. Copying `.env.example` remains sufficient for local demo setup. Past Git history is unchanged. Rotate any credential from the formerly tracked `.env` that was deployed; no external rotation was attempted.
- Compose publishes the chosen host port to container **5432**, preserving the host expression/default (`POSTGRES_PORT`, fallback 5434). `.env.example` retains its existing host-port choice of 5432. The application connects to `postgres:5432` internally.

Production mode is a configuration guard, not a complete production security profile. Demo data, browser bearer storage, local frontend fallback, nonrevocable sessions and other audit findings remain. No commerce endpoint semantics, migration history, legacy UI, authentication transport or schema were changed.

## Validation record and next phase

Local validation uses Go 1.22.1, Node 22.14.0, PostgreSQL 14.24 and a disposable PostgreSQL 16 Docker image. Both database adapters pass the shared HTTP contract checks. The following commands completed successfully (Go commands used `GOCACHE=/tmp/shop-go-cache`; integration runs used `GO=/usr/local/go/bin/go`):

| Command | Result |
| --- | --- |
| `make check` | Formatting, vet, unit and memory OpenAPI/HTTP contract tests, JavaScript syntax passed |
| `make check-api` | OpenAPI validation, route inventory and successful request/response validation passed |
| `make test-race` | All ordinary Go tests passed with race detection |
| `bash scripts/test-integration.sh` | Disposable PostgreSQL 14 suite passed with race detection |
| `PG_BINDIR=/nonexistent bash scripts/test-integration.sh -v` | All 16 top-level PostgreSQL 16 tests and their subtests passed with race detection |
| `go vet -tags=integration ./tests/integration` | Passed |
| `make build` | Production binary built |
| `go mod verify` | All modules verified |
| `bash -n scripts/test-integration.sh` | Passed |
| `docker-compose --env-file .env.example config --quiet` | Installed Compose plugin passed; resolved configuration assertions confirmed host-port preservation and container target 5432 |
| `git diff --check` and `git diff --cached --check` | Passed |

Migration immutability, local `.env` retention, exclusion from the Git index and ignore matching were also verified. The committed workflow has been exercised through its local component commands; a hosted GitHub Actions run is not claimed.

Remaining evidence gaps include browser E2E/accessibility, representative load/query-plan benchmarks, arbitrary cart-update/checkout interleavings, migration contention between deployments, backup/restore rehearsal and dependency vulnerability/support review. The earlier audit remains applicable. Passing tests does not establish a production guarantee.

Recommended next phase: a separately scoped production-safety design and implementation for demo-seed separation, revocable browser sessions/CSRF, abuse controls, serialized release migrations, and reviewed strict purchasing consent/idempotency. Resolve and version intentional contract changes before starting the frontend migration. This phase stops here.

## File inventory

Created:

- `api/openapi.yaml`
- `api/contract_test.go`
- `tests/contract/http.go`
- `tests/integration/database_test.go`
- `tests/integration/migrations_test.go`
- `tests/integration/catalog_test.go`
- `tests/integration/identity_test.go`
- `tests/integration/purchasing_test.go`
- `tests/integration/concurrency_test.go`
- `tests/integration/http_test.go`
- `scripts/test-integration.sh`
- `cmd/server/main_test.go`
- `.github/workflows/check.yml`
- `.gitignore`
- `docs/baseline.md`

Modified: `cmd/server/main.go`, `.env.example`, `docker-compose.yml`, `Makefile`, `go.mod`, `go.sum`, `README.md`, `docs/README.md`.

Removed from tracking only: `.env` (local copy retained). No migration, domain, application service, persistence adapter or legacy frontend source was modified.
