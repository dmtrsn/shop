# ADR 004: Extend PostgreSQL catalog/search behind stable Go contracts

Status: Proposed. Date: 2026-09-25. Depends on ADRs 001 and 003.

## Context

The repository already has generated `tsvector`, GIN full-text/tag indexes, trigram indexes, parameterized filters and allowlisted sorts. Queries cover product name, SKU, series, manufacturer, brand and category. Trigram indexes currently accelerate substring matching; they do not by themselves implement typo tolerance. Facets exist but the frontend does not consume them. No measurements demonstrate a need for an external search engine.

## Decision

Keep PostgreSQL as the initial search implementation behind a focused catalog/search port. Introduce one normalized query specification used consistently by results, facets, suggestions, entity pages and admin lookup. Bound query length, page size, tag count and facet expansion; treat `%` and `_` as literal user characters unless wildcard syntax is intentionally supported.

Implement measured ranking with exact SKU first, then SKU prefix/exact product name, name token/prefix matches, entity matches, and controlled fuzzy matches. Add stock/popularity as modest tie-breakers, never allowing them to bury exact SKU intent. Always include stable product ID tie-breakers. Retain explicit price/name/new sorts; add explicit relevance sorting for queries without silently changing the legacy default.

Normalize Unicode, case, whitespace and SKU punctuation using documented rules; preserve original text and identity. Evaluate Russian morphology, Latin brand names, ё/е behavior and multilingual tokens against a judged query set. Typo fallback must have minimum lengths, thresholds and result caps; exact SKU must remain deterministic. Benchmark weighted full-text ranking and trigram similarity before choosing thresholds. PostgreSQL provides distinct [similarity operators and index-supported matching](https://www.postgresql.org/docs/16/pgtrgm.html); adopting them is a query change, not just an index addition.

## Read models and consistency

Start with existing joins and query plans. If they cannot meet measured targets, introduce `catalog_search_documents` keyed by product ID, with searchable product/SKU/entity text, weighted vector and indexed normalized fields. A generated column on products cannot automatically follow joined manufacturer/brand renames; use explicit transactional updates or an outbox-driven rebuild with revision tracking. Document freshness bounds and verify rebuild completeness before switching reads.

Search projections are disposable. PostgreSQL commerce rows remain authoritative, especially for current stock/price and publication eligibility. Facets/results must apply the same visibility rules. If indexing becomes asynchronous, expose lag metrics and reconcile current product eligibility before showing actionable offers. Do not use search index stock for checkout validation.

Use `EXPLAIN (ANALYZE, BUFFERS)` on a disposable representative dataset, including skewed brands, broad searches and low-selectivity filters. Avoid assuming every trigram/OR query uses an index effectively. Keep page-number navigation for normal catalog browsing; bound excessive offsets and introduce cursor contracts for very deep/admin lists where needed.

## Facets and suggestions

Define new facet semantics explicitly: counts for an axis apply other active filters but exclude that axis's own selection (disjunctive facets). Selected values remain visible even with zero matches. Category counts include descendants. Price bounds exclude the active price constraint. Return bounded common values plus searchable/paged facet-value endpoints for manufacturers, brands and series. Preserve legacy fully-filtered facet behavior through a mode or version until migrated.

Suggestions return typed product, manufacturer, brand, series, category and collection links, with controlled per-type quotas so products cannot crowd out entity navigation. Avoid a full catalog count for an eight-item suggestion request. Give each result stable identity and a navigable target; client clicks must work without prior product loading. Add cancellations and a post-await sequence guard, and follow the accessible combobox pattern. No raw HTML snippets in search responses.

Collections have detail/metadata and an editorial-order sort. Enforce schedule/status on listings, direct lookup and collection filtering. Manufacturer/brand/series directories are separately paged/searchable; a product search is not their replacement.

## Proposed API and schema changes

| Contract | Change |
| --- | --- |
| Existing `GET /api/catalog` | Keep current request/response; add compatible `seriesId`, explicit relevance/editorial sorts when available; preserve legacy aliases |
| New catalog contract | Multi-value axis filters, normalized applied filters, stable ordering and explicit paging; use `/api/v2/catalog` if envelope/semantics differ |
| Facets | New disjunctive mode/version; bounded `GET /api/v2/catalog/facets/:axis?q=...&cursor=...` |
| Suggestions | Bounded typed results for all six entity types; canonical targets and accessibility-friendly plain text |
| Entity endpoints | Manufacturer/brand/series/category/collection details and searchable child lists |
| Database | ADR 003 series/category/collection additions; ranking indexes/projection only after measured plans |

Do not add speculative indexes en masse. Build large indexes with an explicit low-lock migration process, observe write overhead, and retain a rollback path to original queries. Existing `001`–`004` stay immutable.

## Alternatives and consequences

External search could later improve language tooling or scale, but would add synchronization, failure and operation costs now. Revisit only if representative latency/relevance requirements cannot be met economically in PostgreSQL. Keep the search port/read model replaceable so that decision does not affect cart/order logic.

Disjunctive counts and cross-entity ranking increase query complexity. They need query-plan tests, bounded inputs and a relevance evaluation set; SQL injection tests alone are insufficient.

## Acceptance

Proposed baseline dataset: at least 50,000 products, 1,000 manufacturers, multiple brands/series per maker and deep categories; refine with real business volumes. Proposed targets on declared hardware/concurrency: API p95 suggestions ≤150ms, catalog+facets ≤400ms, excluding browser debounce/network. These are goals, not current benchmarks. Track zero-result rate, exact-SKU top result, typo recovery, judged top-five relevance and stale-index lag. Validate pagination determinism, publication schedules and identical filter interpretation across endpoints before rollout.
