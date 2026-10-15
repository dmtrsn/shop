# Design language: «Слой, evolved»

Status: Accepted. Date: 2026-10-11. Applies to the single customer storefront, `apps/storefront`.

## Authority

- **Functional behaviour:** [storefront-ux-spec.md](storefront-ux-spec.md) remains authoritative.
- **Visual language:** this document. It supersedes the conflicting visual direction in
  [storefront-visual-spec.md](storefront-visual-spec.md), whose colour, radius and decoration rules
  rejected the original shop's identity.
- **Values:** `apps/storefront/src/styles/tokens.css` is the only place colours and visual
  constants are defined. This document says how to use them.

The goal is the recognisable evolution of the original `web/` storefront, rebuilt on the Next
architecture. It is not a pixel copy of the old shop and not a new unrelated redesign. `web/`
is a visual reference only; its demo behaviour is never copied (no fake delivery slots, payment
choices, bonuses, ratings, popularity, promo codes, profile or notification saves, or locally
seeded data). A presentation pattern may be reused only for real backend data.

## Identity

| Element | Rule |
|---|---|
| Canvas | Warm off-white `--color-canvas`; surfaces near-white `--color-surface`; quiet bands `--color-surface-subtle`. |
| Ink | Text is dark green-black `--color-text`. The inverse surface `--color-surface-inverse` carries the footer, the Каталог trigger and summary panels (cart/checkout totals). |
| Primary action | Dark ink fill with white text (`--color-accent`, hover `--color-accent-hover`). One primary action per region. |
| Brand lime | `--color-brand` with **ink text only**. Used selectively: the logo triangle, at most one brand CTA per view (hero, "added" confirmation), "new" badges, highlights on dark surfaces. Never as large background fields or body text. |
| Sale accent | `--color-sale` with **ink text only** (white on it is 2.7:1). Sale badges and counters; never decoration. |
| Logo | `LogoMark`: rounded dark square with a lime downward triangle and the lowercase wordmark «слой». Inverted (white square, dark triangle) on dark bands. `app/icon.svg` is the same mark. |
| Photography | Products and the photographic hero carry appetite and colour. The hero is a full-bleed rounded photo card with a dark scrim and light copy (Home slice). |
| Mobile | A fixed bottom tab bar for primary navigation is the target mobile shell (shell slice). |

## Typography

System Inter stack, no web-font download. Body 15px / 1.45 (`--font-size-body`); text-entry
controls stay 16px so mobile Safari does not zoom. Headings are heavy and tightly tracked; sizes
belong to components, never to global element rules.

| Role | Token | Treatment |
|---|---|---|
| Hero | `--font-size-hero` | weight ~690, line-height 0.92, `--tracking-hero` (Home only) |
| Page title | `--font-size-page-title` | `PageHeader`; weight 720, line-height 0.98, `--tracking-page-title` |
| Section title | `--font-size-section-title` | `SectionHeader`; weight 700, line-height 1.02, `--tracking-section-title` |
| Eyebrow / kicker | `--font-size-eyebrow` | weight 760, uppercase, `--tracking-eyebrow`; kicker in `--color-success` |
| Card title | `--font-size-card-title` | `--tracking-card-title` |
| Price | `--font-size-price` | heavy, `--tracking-price`, tabular numerals, no wrapping |

## Geometry and elevation

- Radii: controls `--radius-control` (13px), chips and small controls `--radius-small` (11px),
  cards and panels `--radius-panel` (21px), tiles `--radius-tile` (20px), hero/feature
  `--radius-feature` (32px, 23px on small mobile), pills `--radius-pill`.
- Cards and panels have a 1px border and **no resting shadow**. Independently actionable cards
  may lift on hover (`Panel lift`: `--shadow-lift` and `--lift-distance`, movement only without
  `prefers-reduced-motion`). Menus, dialogs and toasts use `--shadow-overlay`.
- Width: one storefront container, `--container-storefront` 90rem; reading width 45rem; gutter
  `--gutter` (16px on mobile, up to 24px). Sections are separated by `--section-gap`.

## Breakpoints

Three families: **1180px** (`73.75rem`, wide → medium desktop), **900px** (`56.25rem`, desktop →
mobile shell), **640px** (`40rem`, small mobile). New media queries use only these. Existing
queries converge page by page in later slices.

## Shared primitives (`apps/storefront/src/components/ui`)

`Button` / `ButtonLink` (variants `primary` dark, `brand` lime, `secondary` bordered surface,
`quiet` subtle fill, `tertiary` text; sizes `md` 44px and `sm` 38px), `LogoMark`, `Panel`
(`default` / `subtle` / `inverse`), `PageHeader`, `SectionHeader`, `EmptyState` (dashed boundary,
icon tile, one next step), `Skeleton`, and the stroke icon set (1.7 stroke, round caps and joins,
legacy geometry). Pages compose these and keep only page-specific layout in their own CSS Module.

## Accessibility retained from Next

These override legacy values wherever they conflict:

- Focus ring: 3px `--color-focus` with a 3px offset; on inverse surfaces `Panel tone="inverse"`
  switches it to lime (`--color-focus-on-inverse`).
- Muted text stays `#5f655c` (≥4.9:1 on canvas and subtle surfaces); the legacy grey fails AA.
- Success text is `#2a7654`, the nearest AA tone to the legacy `#2d7d58` (4.48:1 on canvas).
- Form-control boundaries use `--color-border-control` (3:1 non-text contrast), not the lighter
  decorative borders.
- Warning and error text keep Next's darker AA tones; legacy orange is a fill only.
- Status is never colour alone; empty, unavailable and error states stay honest and distinct.
- Reduced motion disables lifts, shimmer and smooth scrolling.

## CSS rules

- No hex values outside `tokens.css`, except metadata that cannot read CSS variables
  (`app/icon.svg`, `viewport.themeColor`).
- No Tailwind or other styling framework, no copied `web/styles.css`, no global component classes.
- No `!important` except the documented reduced-motion and visually-hidden utilities.
- Prefer tokens over magic numbers; shared visual treatment lives in the primitives.
- Inline `style` attributes are not used: the nonce CSP blocks server-rendered ones.
