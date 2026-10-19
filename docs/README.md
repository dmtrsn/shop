# Architecture and product documentation

Review date: 2026-09-25. Baseline: `7be70d7`.

These documents describe the approved architectural and product direction for the project. Some documents are proposals for future migration work, while implemented baseline changes are documented separately.

- [Repository audit](repository-audit.md): current architecture, preserved behavior, gaps, security findings, and verification limits.
- [Migration plan](migration-plan.md): sequence, parity gates, rollback, future structure, and anticipated file changes.
- [Storefront UX specification](storefront-ux-spec.md): product and interaction specification for the future React/Next.js storefront.
- [Design language](design-language.md): the authoritative «Слой, evolved» visual language, tokens and shared primitives.
- [Storefront visual specification](storefront-visual-spec.md): visual system, responsive composition, homepage hierarchy, and visual guardrails for the future storefront; its visual direction is superseded by the design language.
- [ADR 001: Target architecture](adr/001-target-architecture.md).
- [ADR 002: Frontend migration](adr/002-frontend-migration.md).
- [ADR 003: Information architecture](adr/003-information-architecture.md).
- [ADR 004: Catalog and search](adr/004-catalog-search-architecture.md).
- [ADR 005: Cart and orders](adr/005-cart-order-architecture.md).
- [ADR 006: Administration](adr/006-admin-architecture.md).
- [ADR 007: Separate staff identity](adr/007-staff-identity.md).
- [ADR 008: Sign-in rate limits at the edge](adr/008-edge-sign-in-rate-limits.md).
- [ADR 009: Transactional email foundation](adr/009-transactional-email.md).
- [ADR 010: Customer self-registration](adr/010-customer-self-registration.md).
- [ADR 011: Self-service password recovery](adr/011-customer-password-recovery.md).
- [ADR 012: Transactional staff audit](adr/012-transactional-staff-audit.md).

The architectural recommendation is to preserve the Go application and PostgreSQL, introduce a TypeScript/React storefront with Next.js, and replace complete routes incrementally. Keep the existing frontend available until each replacement meets behavioral, accessibility, security, and performance gates. Administration becomes a separate React application using authorized Go use cases.

The storefront UX and visual specifications complement those architectural decisions. They define navigation, catalog, search, product, collection, responsive behavior, visual hierarchy, accessibility, homepage composition, and large-order interaction patterns. They deliberately do not redefine backend commerce contracts or the complete cart/order workflow.

## Implementation baseline

The Production Baseline and Contracts phase is recorded in [baseline.md](baseline.md), with the implemented HTTP contract in [../api/openapi.yaml](../api/openapi.yaml). The repository audit and ADRs remain the architectural reference; future APIs described there are not implied to be implemented by the baseline phase.
