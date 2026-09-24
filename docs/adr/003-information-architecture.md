# ADR 003: Keep category, producer hierarchy, collections and search independent

Status: Proposed. Date: 2026-09-25. Depends on ADR 001.

## Context

The backend already distinguishes categories, manufacturers, brands and collections. The frontend partially collapses these into a category mega-menu generated from one discovery page. Series have no stable identity. This cannot represent thousands of products or many manufacturers reliably.

## Decision

Offer four first-class discovery paths sharing one catalog query model:

| Axis | Purpose | Navigation |
| --- | --- | --- |
| Category tree | What kind of product | Category → descendants → filtered products |
| Producer hierarchy | Who makes it and in which range | Manufacturer → brands → series → products |
| Collections | Editorial occasion/theme/selection | Collection directory → collection landing → products |
| Search | Known item and exploratory intent | Query → suggestions/results → any entity or product |

Use a prominent global search, a compact category entry, a separately searchable manufacturers directory, and a visible collections entry. Do not expose every entity simultaneously in a mega-menu. Provide A–Z/А–Я lookup, bounded lists, counts and breadcrumbs. Manufacturer names and brands must not be presented as interchangeable concepts.

Proposed public routes are `/catalog`, `/categories/:slug`, `/manufacturers`, `/manufacturers/:slug`, `/brands/:slug`, `/brands/:brandSlug/series/:seriesSlug`, `/collections`, `/collections/:slug`, `/products/:slug`, `/search?q=...`, `/favorites`, `/cart`, `/account/orders`, and `/account/orders/:id/repeat`. A series slug is scoped to its brand. Product canonical URLs do not depend on category placement. Preserve old IDs/slugs and map legacy hash links as described in ADR 002.

Entity pages use IDs to query, slugs to navigate, and names to display. They show related entities with counts and a product list without requiring every intermediate click. Category and manufacturer filters can coexist; show active constraints and let customers clear any axis independently. Changing a parent clears incompatible brand/series selections visibly. Deep links validate contradictory parent/child combinations instead of silently broadening results.

Within a filter axis, future multi-select values use OR; across axes use AND. Tags need explicit semantics: preserve existing all-tags matching unless a new mode is requested. Differentiate category descendants from direct assignments. Back/forward restores query, filters, sorting, page and appropriate scroll/focus.

## Database and API implications

- Preserve category adjacency (`parent_id`) and add recursive descendant/ancestor reads, cycle prevention and a parent index. Category deletion/reparenting must be an explicit admin operation with impact preview; do not rely on `ON DELETE SET NULL` as editorial behavior.
- Add `series(id, brand_id, slug, name, description, sort_order, status, created_at, updated_at)` with uniqueness within brand. Add nullable `products.series_id`; backfill by `(brand_id, normalized series name)`, preserving original display text. Identical fixture labels already occur under different brands and must create separate scoped identities unless editors deliberately reconcile them.
- Preserve the existing product brand/manufacturer composite FK. Add series/brand consistency, e.g. a unique `(id, brand_id)` on series referenced by `(series_id, brand_id)` on products. Products may have no series. Represent unbranded products deliberately with manufacturer-specific unbranded brands initially, rather than assigning all to the existing `m-choco` placeholder.
- Keep old `series` JSON text and query semantics during transition; add `seriesId` and a separate structured series field. Do not change an existing string into an object in place.
- Add entity detail endpoints and bounded searchable children/directories. Existing all-items endpoints remain compatible until their clients retire. Do not derive available manufacturers/brands/series from loaded product pages.
- Extend collections with draft/published/archive status, publication policy and metadata. Preserve `collection_products` and manual ordering. Enforce schedules consistently in direct access and catalog filters. Rule-driven collections can follow as a separate feature.
- Add slug history/redirects only when rename operations need them; changing names must not break stable IDs or historical order snapshots.

## Large-list navigation and visual rules

Cart and favorites provide search by name/SKU/manufacturer/brand/series, availability/selection filters, manufacturer/category/brand grouping, collapse/expand and compact rows. Clearly label whether bulk selection affects visible rows, a group, or the full filtered result. Hidden selected items remain selected and visible in summary counts. Cart totals reflect checkout selection, not merely currently displayed rows.

Use whitespace on discovery pages and higher density in work lists. Keep a consistent typographic hierarchy and warm neutral surfaces; reserve color for action, selection and status. Use product imagery that distinguishes actual SKUs. No excessive pink, candy gradients, childish graphics, pervasive rounded cards or a generic dashboard template for the shop.

Mobile navigation keeps search reachable, filters in an accessible drawer, and a compact sticky cart total/checkout action that respects the keyboard and safe area. Prefer semantic links, native controls and disclosure patterns to oversized tree widgets; preserve focus and selection through filtering.

## Alternatives and consequences

A single category/manufacturer mega-tree duplicates relationships and becomes unmanageable. Treating collections as tags loses editorial identity. Treating series as free text prevents reliable ownership and navigation. Separate axes introduce URL and consistency rules, but reuse the same catalog engine and allow direct discovery.

## Acceptance

Validate a three-plus-level category tree, two brands with identically named series, a manufacturer with many brands, a product without series, an expired collection, and a catalog with thousands of products. Every navigation path must resolve through API data, preserve compatible filters and open a product outside the first page. Test keyboard and mobile navigation without requiring hover. Existing IDs, collection memberships and order history remain intact after backfill.
