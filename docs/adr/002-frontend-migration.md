# ADR 002: Replace storefront routes incrementally with TypeScript and React

Status: Proposed. Date: 2026-09-25. Depends on ADR 001.

## Context

`web/app.js` combines routing, rendering, fixtures, transport and mutable state. Full DOM replacement makes focus and concurrent mutations fragile. However, the frontend already supports useful catalog, cart, favorites and repeat workflows. It must keep working while replacements are developed.

## Decision

Create `apps/storefront` using TypeScript, React and Next.js App Router, alongside untouched legacy files initially. Migrate complete route families through an explicit route ownership configuration. Avoid two frameworks controlling the same DOM or separate cart implementations writing simultaneously. Use React components for forms, search, tables/lists and interactive controls; use server rendering for discoverable public pages and metadata. Select and pin supported versions when implementation starts.

Use one server-state query layer, request cancellation and typed API access. Keep catalog filters in URLs, authentication on the server, and UI preferences/drafts in small feature-local state. Use a product map keyed by canonical ID, not an ever-growing fixture-enriched array. Optimistic changes must reconcile with server versions; financial confirmation always uses server results. Serialize rapid quantity changes per line and coordinate aggregate versions across lines/tabs.

## Coexistence and URL ownership

Hash fragments are invisible to the reverse proxy. `/#/catalog` cannot be routed to Next solely by edge configuration. Retain `/` as legacy initially and introduce real paths such as `/products/:slug`, `/collections/:slug`, `/catalog`, `/cart` as they pass parity. Use a small legacy navigation adapter later to map only migrated hash routes to those paths. Preserve filter aliases and values explicitly, including current price units and sort aliases.

Keep a dedicated `/legacy/` entry as root ownership moves. Its root-absolute `/app.js`, `/styles.css` and `/assets/*` references must remain served from legacy while needed. Reserve `/_next/*` for Next and separate admin build assets. The old root/hash bridge must continue handling bookmarks after the homepage migrates. Unknown routes must produce intentional 404s, not leak into another application's fallback.

Cross-application navigation uses full document links with preserved query state; within Next, use its router. Do not rely on soft navigation into legacy routes. Route-level reversibility follows the same principle as [Next.js multi-zone navigation](https://nextjs.org/docs/app/guides/multi-zones), although this project needs only one Next storefront plus the legacy app, not multiple Next zones.

## Sessions and customer data

Before authenticated route cutover, teach the legacy client and Go API the new cookie session contract, including CSRF headers and logout revocation. Keep bearer acceptance for a bounded compatibility window. Prefer reauthentication; if a one-time token exchange is chosen, validate the bearer in Go, mint a new session, revoke the exchanged token and erase browser storage. Never expose tokens in links. Rollback targets the compatible legacy build, not the original insecure session transport. Staff authentication is separate.

Guest-cart behavior requires its own gate: remove automatic demo prepopulation in production, validate persisted guest lines against the API, retain lines outside page one, and preview the merge into an account. Show conflicts and quantities, commit once, then clear only successfully transferred guest data. Until guest handling reaches parity, do not route guest purchasing through the new application. Use temporary localStorage only for non-sensitive guest drafts/preferences, version its format, and namespace it independently from authenticated data. A server-backed anonymous cart can follow without being required for the first public read route.

Invalidate authenticated query caches and cancel pending work on logout/account switch. On returning from a legacy route, reload server cart/favorites/badges. Both apps use the same account identity and Go persistence; no dual writes to separate stores.

## Slice sequence

1. Document contracts and browser behavior; add test fixtures and regression coverage before adapting legacy navigation.
2. Add isolated frontend build, design tokens, accessible primitives, same-origin routing and session compatibility.
3. Introduce a product detail route as a pilot. Its add/favorite controls must use existing APIs and obey guest/session gates. Then collections and independent manufacturer/brand/series pages.
4. Replace search/catalog together with backend filters/facets, preserving pagination, loading/error recovery and URLs.
5. Replace favorites, then cart and checkout, with batch/version contracts available first.
6. Replace history and repeat review only after explicit confirmation, stock, price and retry tests pass. Keep the existing repeat service.
7. Migrate remaining public/account pages, move root ownership, retain hash bridge and legacy rollback for an agreed observation window, then retire legacy assets.

## UX and quality gates

- Use restrained warm neutrals, strong typography, compact row variants and modest corner radii. Share tokens rather than the entire global legacy CSS. Avoid global selectors leaking between apps.
- Search supports keyboard navigation, active option announcement, loading/empty states and direct URLs for every suggestion type. Inputs/drafts retain focus through refetches.
- Every slice passes manual keyboard/mobile checks, screen-reader spot checks, automated accessibility checks and reduced-motion behavior. Target WCAG 2.2 AA; this is a future acceptance target, not a current compliance claim.
- Capture performance baselines. Proposed release targets: p75 LCP ≤2.5s, INP ≤200ms, CLS ≤0.1; verify on defined mobile/network profiles. Establish JS/image budgets from the pilot and prevent regressions in CI.
- Exercise 100, 500 and 1,000 cart/favorite lines. Prefer stable keyed rows and bounded reads first; virtualize only after profiling and preserving keyboard, screen-reader and browser-find behavior.
- Maintain contracts for unavailable deselection, quantity steps, totals, cross-app badges and logout. No successful-looking local fallback when production APIs fail.

## Alternatives, consequences and rollback

A single-page React rewrite would remove the old UI before parity; rejected. React islands are possible for a genuinely isolated widget, but using them throughout the current global renderer risks DOM ownership conflicts. Route replacement is the default. Temporary hard navigations and two builds are acceptable costs.

Rollback changes route ownership to the compatible legacy app. Keep additive API/schema changes; do not reverse data migrations. Preserve safe guest drafts and in-progress review data where possible, but never replay mutations automatically after navigation or an ambiguous network result. Test root/hash redirects, canonical URLs, cookies, CSP, static asset routing and cached pages in both directions before each cutover.
