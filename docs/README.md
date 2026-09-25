# Architecture review

Review date: 2026-09-25. Baseline: `7be70d7`.

These documents are proposals, not an implemented migration or accepted production design. The review changes documentation only.

- [Repository audit](repository-audit.md): current architecture, preserved behavior, gaps, security findings, and verification limits.
- [Migration plan](migration-plan.md): sequence, parity gates, rollback, future structure, and anticipated file changes.
- [ADR 001: Target architecture](adr/001-target-architecture.md).
- [ADR 002: Frontend migration](adr/002-frontend-migration.md).
- [ADR 003: Information architecture](adr/003-information-architecture.md).
- [ADR 004: Catalog and search](adr/004-catalog-search-architecture.md).
- [ADR 005: Cart and orders](adr/005-cart-order-architecture.md).
- [ADR 006: Administration](adr/006-admin-architecture.md).

The recommendation is to preserve the Go application and PostgreSQL, introduce a TypeScript/React storefront with Next.js, and replace complete routes incrementally. Keep the existing frontend available until each replacement meets behavioral, accessibility, security, and performance gates. Administration becomes a separate React application using authorized Go use cases.

## Implementation baseline

The Production Baseline and Contracts phase is recorded in [baseline.md](baseline.md), with the implemented HTTP contract in [../api/openapi.yaml](../api/openapi.yaml). The earlier audit and ADRs remain the architectural reference; future APIs described there are not implemented by this phase.
