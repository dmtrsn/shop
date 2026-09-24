# ADR 006: Build a separate administration application over Go use cases

Status: Proposed. Date: 2026-09-25. Depends on ADRs 001, 003, 004 and 005.

## Context

There is no current administration application, staff identity, write API for catalog entities, audit log, import/export or analytics system. Public read models expose published-style data and omit some zero-product brands. Extending shopper endpoints into an admin API would mix privileges and workflows.

## Decision

Create `apps/admin`, a TypeScript/React application with an independent build and deployment at a staff origin. A client-rendered application is sufficient; use the same React primitives/tokens and generated API client as storefront where appropriate. Implement `/api/admin/v1/*` in Go with dedicated DTOs and application authorization. Client route guards are convenience only. No browser or Next direct DB access and no generic table-to-CRUD exposure.

Use dense readable tables, saved views, search, filters, configurable columns, keyboard editing, selection across pages and explicit bulk scope. Put operations needing attention on the dashboard; avoid decorative metric tiles and a generic SaaS dashboard skin. Reuse restrained typography/color while making administration visibly distinct from shopping. Destructive/large actions have an impact review and explicit commit; routine reversible edits need not produce repeated confirmation dialogs.

## Modules and workflows

| Area | Initial useful behavior |
| --- | --- |
| Dashboard | Orders needing action, stock exceptions, failed imports/jobs, dated summaries with definitions |
| Products | Search/SKU lookup, edit, draft/publish/archive, images, category/brand/series assignment, quantity rules, revisions |
| Manufacturers | Searchable directory, details and brand ownership |
| Brands and series | Ownership-aware editing; duplicate-name handling; series managed as entities under brands |
| Categories | Tree editing with cycle/reparent impact validation and descendant product counts |
| Collections | Metadata, membership/order, schedule, draft preview and publication |
| Inventory | Stock adjustments with reason, movement history, low-stock views; shared transaction rules with checkout |
| Orders | Paged queues, details, validated status transitions and fulfillment actions; never overwrite purchased snapshots |
| Customers | Scoped profile/order access, account status, support workflows and audited exports |
| Bulk operations | Explicit IDs or versioned filter snapshot, impact count, validation preview, commit/results |
| Import/export | Schema mapping, dry run, row errors, review/commit, job progress and downloadable reports |
| Audit log | Actor, action, subject, time, request ID and redacted change history |
| Analytics | Defined sales/order/product metrics, time ranges, timezone, freshness and restricted access |

## Identity, authorization and audit

Add staff principal/role/permission tables or equivalent scoped mappings, with least privilege for catalog, inventory, fulfillment, support and administration. Permissions are action/resource-specific; customer status alone never grants staff access. Use a separate revocable staff session/audience, shorter configured timeouts, MFA through an appropriate identity integration, and reauthentication for sensitive actions. Staff account provisioning and recovery must be defined before write screens are exposed.

Every Go read/write checks permissions, including export and job polling/download access. Record staff writes and sensitive access in an append-only audit store. Mutations and their audit records commit together; redact credentials, session values and unnecessary customer data. The runtime cannot modify/delete audit rows through normal APIs; retention/export access is separately controlled. Enforce optimistic revisions on edits to prevent lost updates.

## Jobs, import/export and media

Use durable PostgreSQL jobs initially, processed by `cmd/worker` when needed. Jobs have state, actor, progress, retries, deduplication, bounded batches and resumable checkpoints. A worker uses the same domain/application validation as HTTP, not unvalidated SQL inserts. Admin inventory changes serialize with checkout and append movements.

Imports stage rows, validate SKU uniqueness and hierarchy/rules, return errors, and show a dry-run diff. Commit references the reviewed file digest and entity revisions. Small batches may be atomic; large imports use explicitly documented chunk commits with row outcomes and compensating actions, never a misleading all-or-nothing promise. Preserve original files only for defined retention; resumptions must not duplicate writes. Exports run under scoped authorization, redact fields appropriately and protect spreadsheet consumers against formula injection.

Media is stored outside the application image with controlled upload/type/size validation and generated derivatives. Use object metadata and content-addressed public assets; private import/export objects require short-lived authorized access. Do not allow arbitrary HTML or unrestricted remote image URLs to become executable/admin content.

## Proposed API and database changes

- Read/detail/write routes under `/api/admin/v1/products`, `/manufacturers`, `/brands`, `/series`, `/categories`, `/collections`, `/inventory`, `/orders`, `/customers`.
- Dedicated command routes for publication, archival, stock adjustment and order transitions instead of unrestricted field updates.
- `/bulk/preview`, `/bulk/commit`, `/imports`, `/exports`, `/jobs/:id`, `/audit-events`, `/analytics/*` with permissions, bounded pagination and explicit status/error contracts.
- Staff identity/roles/permissions, audit events, staged import rows, jobs/outbox, media metadata, entity revisions and inventory movements. Add schemas as their workflows ship, not as empty speculative tables.
- Analytics initially uses indexed read queries or refreshable aggregates with defined event/status/currency semantics. Avoid heavy unrestricted reporting on checkout's critical query path. Revisit read replicas or a warehouse after measured need.

## Alternatives and consequences

Embedding an admin screen in the storefront bundles staff concerns into public navigation and deployment. Exposing DB tables directly bypasses ownership, stock and audit invariants. Both are rejected. A separate app costs another build but allows independent staff access policies and releases; it does not require a separate commerce backend.

## Adoption and acceptance

Start with staff identity, permissions and audit, then read-only products/orders/dashboard. Add catalog editing and inventory commands, then fulfillment/customer workflows, then reviewed bulk/import/export and analytics. Admin work may proceed alongside storefront migration only after shared authorization/schema foundations exist.

Test permission denials and object scope for every module, staff/customer session isolation, immutable order snapshots, concurrent edit conflicts, audit atomicity, import dry-run/commit parity, retry/resume without duplicate stock movement, safe exports and large-table keyboard/mobile usability. No staff write route is ready merely because its UI is hidden from shoppers.
