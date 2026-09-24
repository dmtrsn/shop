# Incremental migration plan

Date: 2026-09-25. Status: Proposed. No implementation is included in this review.

## Delivery rules

Preserve the Go module/import paths, PostgreSQL data, current APIs and legacy frontend initially. Each phase is independently reviewable and deployable. Backend correctness/security changes and frontend replacement should be separate releases where possible. A frontend rollback uses a compatible legacy build and keeps additive schema changes. Never use destructive down migrations as an automatic application rollback.

Before coding a phase, baseline current behavior and distinguish intentional product improvements from accidental contract drift. Known defects and demo placeholders are not parity requirements, but changing them must be explicit. No flag can disable authorization, stock validation or mandatory confirmation guarantees in a production-ready route.

## Phases and exit gates

| Phase | Work | Exit evidence / rollback |
| --- | --- | --- |
| 0. Baseline and contracts | Capture `/api` schemas/errors/aliases; inventory legacy routes and actual vs placeholder UI. Add PostgreSQL integration/migration/concurrency and browser smoke coverage. Establish representative catalog/cart fixtures and performance baseline. | Existing tests remain green; actual PostgreSQL migrations and checkout/repeat pass; store fixture expectations. Documentation/test additions are independent of UI. |
| 1. Production safety | Explicit demo/production mode; secret hygiene; compatible cookie/CSRF/revocation support; login abuse controls; shared account lifecycle; correct DB port mapping; serialized migration job; observability and backups. Harden strict repeat/checkout confirmation and idempotency contracts; adapt legacy purchasing before public launch. | No known demo account enabled in production, missing DSN fails, logout invalidates sessions, CSRF and session tests pass, explicit review/retry cases pass. Rollback to the compatible legacy build without dropping security schema. |
| 2. Data and API foundations | Add series identity with brand-scoped backfill; recursive categories; publication/collection rules; compact/versioned list and batch contracts; search query/ranking/facet improvements after measurement. Keep legacy DTOs and text fields. | Constraint/backfill validation, legacy API regression tests, representative query plans, strict stock/concurrency tests. Switch query/read paths back if necessary; preserve additive columns. |
| 3. Frontend foundation/pilot | Add TS/React/Next app, shared tokens/primitives/client, route ownership, `/legacy/` access and hash mapping design. Establish shared sessions and guest merge. Pilot a complete product detail route, then collections/producer pages. | Direct links, unknown suggested products, add/favorite controls, cross-app sessions/badges/guest data, mobile/keyboard/CSP and cache isolation pass. Toggle route ownership back. |
| 4. Discovery | Replace global search/catalog/navigation with real entity APIs, facets, exact axis filters and accessible suggestions; roll out homepage after supporting routes. | Search judged set, URL/back-forward restoration, deep categories, many manufacturers, collection schedules, perf and accessibility targets. Retain old catalog route as fallback. |
| 5. Large favorites/cart | Ship bounded batch APIs first, then dense list UIs with grouping/search/filters/collapse/select/actions/quantity edits/compact mode/sticky mobile summary. | 100/500/1,000-line fixtures; precise scope/counts, current totals, conflict recovery, out-of-stock deselection, cross-tab updates and guest merge parity. API remains legacy-compatible. |
| 6. Purchasing/history/repeat | Replace checkout and history, then full repeat review using the existing Go use cases and strict contracts. Show price deltas, current rules, explicit choices and retry recovery. | Preview makes no mutation; empty/foreign/duplicate/stale review commits fail; idempotent retry; stock race tests; order snapshots and purchased-line deletion preserved. Roll back UI to strict-contract-compatible legacy build. |
| 7. Administration | Staff identity/audit first; read-only dashboard/catalog/orders; entity editing and inventory; fulfillment/customer operations; reviewed bulk/import/export; analytics. | Per-action authorization, audit completeness, stock ledger/checkout concurrency, import dry-run and resumability, job ownership and accessibility. Disable individual new command routes without altering customer data. |
| 8. Consolidation | Move final route ownership, maintain legacy bookmark bridge, retire unused old UI after an agreed observation window; remove deprecated API/columns only in subsequent releases. | Route usage and error metrics show no remaining legacy clients; restore/rollback exercised; supported dependency/toolchain and release checks pass. Archive migration decisions and preserve old order data. |

Phase 7 can run alongside phases 4–6 once shared identity, catalog consistency and transaction foundations are ready. Do not make the storefront wait for all analytics features. No duration estimates are offered without team capacity and deployment requirements.

## Expand / backfill / switch / contract

For series, create the new table and nullable `products.series_id`, backfill by brand plus normalized text, and report ambiguities. Validate series/brand/manufacturer consistency before using the new hierarchy. Continue serving the original series text to legacy clients and preserve the old query interpretation. Add controlled writes that maintain both representations during coexistence; make the new ID authoritative only after all writers are upgraded. Remove redundant text only after legacy consumers are retired, if removal is still worthwhile.

For cart revisions and strict repeat, add aggregate/review/idempotency tables first, deploy Go support, then adapt legacy and React clients. Every writer must participate in the version/lock protocol before revision checks can be relied upon. Do not cache or queue mutation replay in the browser. Keep a stable idempotency key through uncertain network results.

For production fixtures, preserve historical migration checksums. A new migration runner should separate explicitly demo-tagged seeding from production initialization without silently redefining an existing version as applied. Existing seeded databases require an inspected forward transition (disable demo access and handle fixture data deliberately), not running `002_demo_seed.down.sql`. Fresh production baselining/seed separation needs its own migration test and documented history strategy.

For indexes on populated tables, assess lock duration first. Use concurrent index builds only after the runner can represent nontransactional steps and retries. Plan backups, verified restore, row-count/FK checks and backfill resumability. Application rollback does not undo a successful customer transaction or safely reverse a destructive schema change.

## Future project structure

This is a proposed layout, not directories created in this turn. Keep Go at the root to avoid needless import/module churn. Extract files within the current layers as features change; do not scaffold every module immediately.

```text
cmd/
  server/main.go                 existing API composition root
  migrate/main.go                explicit release migration entry point
  worker/main.go                 added when durable jobs ship
internal/
  domain/                       existing models/errors; split rules by capability
  application/
    identity/ catalog/ cart/ favorites/ orders/ inventory/ admin/
  delivery/
    httpapi/                    routes, DTOs, middleware, contract adapters
    jobs/                       job handlers when required
  repository/
    postgres/                   focused SQL adapters and transaction implementations
    memory/                     demo/test adapter, not production fallback
  platform/                     configuration, telemetry, media and job plumbing
migrations/                     retain existing versions; append tested changes
api/
  openapi.yaml                  authoritative current/evolving API contract
apps/
  storefront/
    src/app/                    Next public/account route boundaries
    src/features/               catalog, search, cart, favorites, orders, repeat
    src/components/             storefront composition
    src/lib/                    API/query adapters, URL and session helpers
    public/
    package.json
    next.config.ts
  admin/
    src/app/                    staff router, auth boundary, layout
    src/features/               staff modules, tables and operation workflows
    package.json
packages/
  api-client/                   generated TS DTOs/client plus thin transport
  ui/                           accessible primitives, no commerce policy
  design-tokens/                color, typography, spacing, density, motion
  config/                       shared TypeScript/lint configuration
web/                            existing frontend until parity/retirement
tests/
  integration/                  disposable PostgreSQL scenarios
  e2e/                          legacy + new + coexistence browser scenarios
  fixtures/                     deterministic catalogs and large customer lists
  performance/                  declared load datasets and thresholds
deploy/
  proxy/                        explicit route/static/API ownership
docs/
  adr/
.github/workflows/              contract, Go, frontend and migration checks
package.json                    frontend workspace commands
pnpm-workspace.yaml             proposed workspace manager configuration
pnpm-lock.yaml                  reproducible frontend dependency resolution
```

Exact frontend package versions and supporting libraries are implementation-time choices. A workspace package manager and simple CI are enough initially; a task-orchestration platform is not required. Domain/application boundaries should be explicit imports/interfaces, not just folder names.

## API/database work register

| Capability | API work | Schema work | Timing |
| --- | --- | --- | --- |
| Identity | Cookie login/session, logout/revoke, CSRF, account recovery later; bounded bearer compatibility | Revocation/idle/session metadata and account status; staff identities separately | Before authenticated cutover/public launch |
| Hierarchy | Entity details and bounded directories/children; series ID alongside string | `series`, `series_id`, composite FK, category parent index/cycle validation | Before producer navigation |
| Collections | Detail/publication/editorial ordering; consistent eligibility | Status/metadata and schedule validity; keep membership table | Before collection cutover |
| Search/facets | Explicit relevance, fuzzy matching, typed suggestions, disjunctive bounded facets | Targeted indexes; search projection only if measured need | Before discovery cutover |
| Cart/favorites | Compact/paged lists, stable versions, explicit bulk scopes and outcomes | Aggregate revisions; preserve existing user/product uniqueness | Before large-list cutover |
| Repeat | Required review identity + explicit items + idempotency; structured changed-review response | Expiring review snapshots and atomic idempotency records | Harden legacy early; retain use cases |
| Checkout/orders | Authoritative quote/confirmation, idempotency, paged history/details | Immutable currency/financial breakdown/contact fields, quote state where needed | Before purchase cutover |
| Admin/inventory | Authorized command APIs, jobs, audit, imports/exports, analytics | RBAC/audit, movements, entity revisions, jobs/staging/media/outbox as needed | Incremental staff modules |

Payments, multiple warehouses, supplier integrations, tiered pricing, customer organizations and external search are not existing implemented features. Resolve their requirements before selecting infrastructure or adding schema. They need not block the first migration slices.

## Exact files for this turn

Create only these nine documentation files:

1. `docs/README.md`
2. `docs/repository-audit.md`
3. `docs/migration-plan.md`
4. `docs/adr/001-target-architecture.md`
5. `docs/adr/002-frontend-migration.md`
6. `docs/adr/003-information-architecture.md`
7. `docs/adr/004-catalog-search-architecture.md`
8. `docs/adr/005-cart-order-architecture.md`
9. `docs/adr/006-admin-architecture.md`

No existing application, schema, build or environment file is changed. No package is installed. ADRs remain Proposed.

## Anticipated implementation files, not changed now

The first implementation batch should be scoped to baseline contracts/tests and production safety, not the whole future tree:

- Create `api/openapi.yaml`, `tests/integration/migrations_test.go`, `tests/integration/purchasing_test.go`, `tests/integration/identity_test.go`, `tests/e2e/legacy.spec.ts`, and a CI workflow such as `.github/workflows/check.yml` with the necessary isolated test tooling.
- Create `.gitignore` and eventually untrack `.env` while retaining local developer configuration; update `.env.example`, `README.md`, `Makefile`, `Dockerfile`, `docker-compose.yml` as explicit operational changes.
- Modify `cmd/server/main.go` for environment validation; add `cmd/migrate/main.go`; evolve `internal/repository/postgres/migrate.go` for safe release execution.
- Modify `internal/domain/repository.go`, `internal/domain/models.go`, `internal/application/service.go`, `internal/delivery/httpapi/server.go`, `internal/repository/postgres/store.go` and `internal/repository/memory/store.go` for compatible session/contract changes, extracting focused files when warranted; extend existing tests.
- Modify `internal/repository/postgres/cart.go`, `internal/repository/postgres/orders.go` and application repeat code only when adding the version/idempotency/revalidation protocol, keeping batch transaction guarantees.
- Make narrowly scoped future changes to `web/app.js` for session compatibility, explicit demo mode, guest preservation, strict purchasing and migrated-route dispatch. Change `web/index.html`/`web/styles.css` only where shared compatibility/accessibility work requires it; do not re-skin legacy as a prerequisite.
- Append numbered migrations beginning after `004` for the selected phase. Allocate exact migration numbers in implementation order; no SQL migration is authored by this audit.
- Introduce `apps/storefront/package.json`, `apps/storefront/next.config.ts`, `apps/storefront/src/app/layout.tsx`, shared packages and proxy configuration in the frontend foundation phase; introduce `apps/admin` after staff security foundations.

## Risks and controls

| Risk | Control |
| --- | --- |
| Cookie/bearer coexistence and CSRF regressions | Go-owned sessions, explicit transport window, origin/CSRF tests and compatible rollback build |
| Lost guest cart or account bleed | Versioned guest draft, explicit merge review, session-keyed caches and multi-tab tests |
| Hidden behavior lost in a rewrite | Preserve current regression tests, baseline flows, per-route parity gates |
| Repeat retry duplicates or changed-price surprise | Transactional idempotency, reviewed price/rule revisions, explicit reconfirmation |
| Hierarchy backfill assigns wrong owner | Brand-scoped series mapping, ambiguity report, composite constraints |
| Database locks or concurrent migration races | Release job lock, measured backfills/indexes, restore rehearsal |
| Large list/network explosion | Compact reads, bounded batches, profiling and accessible rendering strategy |
| Next caches expose account data | Public/private fetch separation, no shared caching of authenticated responses, isolation tests |
| Old hash URLs/static assets break | Client hash bridge, explicit route ownership, root/static rollback tests |
| Admin bypasses commerce invariants | Go authorization/use cases, audit and inventory transactions shared with checkout |
| Placeholder UI mistaken for working capability | Label/remove demos in production; release real payment/delivery/profile workflows only with backend support |

Open product decisions include expected peak load, maximum order size, guest checkout policy, staff identity provider, delivery/payment providers, batch/expiry inventory needs, legal/localization requirements and analytics definitions. These are planning inputs for later phases, not reasons to replace working backend behavior now.
