# Storefront UX specification

Status: Proposed. Date: 2026-09-25. Applies to the future React/Next.js customer storefront.

## Purpose and product principles

The storefront serves two modes without splitting into separate customer experiences:

1. **Browse:** a customer compares a small number of confectionery products through imagery, category, producer, and editorial context.
2. **Build:** a repeat or wholesale-like customer finds known items and adds 50–200+ different positions quickly and accurately.

Pages should feel modern, premium, calm, and easy to scan. Discovery pages use generous but controlled whitespace; catalog and order-building surfaces can become deliberately dense. Product imagery, names, prices, and availability outrank decoration. The interface must not resemble a childish candy shop or a generic SaaS dashboard: avoid excessive pink, candy gradients, pervasive pill shapes, oversized decorative panels, needless animation, and a separate card around every fragment.

The current storefront supplies useful baseline behavior to preserve: prominent global search and `Cmd/Ctrl+K`, category and producer discovery, catalog filters and pagination, active-filter chips, grid/list choice, minimum and quantity-step handling, favorites, grouped long-list controls, visible feedback, and explicit repeat-order review. The future UI must use canonical routes and authoritative API data rather than the legacy fixture-enriched product registry. Existing profile, promo, payment, address, city, and delivery controls that are only placeholders are not requirements.

## 1. Global storefront structure

### Desktop

Use a compact two-level header only when service information is real and actionable. The optional utility row may contain delivery region/status and a help link; it must not push the main navigation below the first viewport unnecessarily.

The primary header order is:

1. logo linking to the homepage;
2. **Catalog** trigger;
3. **Manufacturers** link;
4. **Collections** link;
5. global search, visually the widest and most prominent control;
6. favorites;
7. account, whose menu exposes orders when signed in;
8. cart, with a position count rather than an ambiguous unit count.

Search remains visible at laptop and wider sizes; it is never buried inside Catalog. Search is the highest-priority discovery control. Cart is the highest-priority transactional utility. Account/orders and favorites follow. Header badges use short counts; large counts may use `99+`, while the destination page gives exact positions and units. The sticky header may compact vertically after scrolling, but its search access and cart state remain available.

Catalog, Manufacturers, and Collections are peer entry points. “Catalog” means the category-led “what” axis, not every possible storefront destination. Account and Orders may share one entry because both are personal destinations; opening the account menu must not be required to see cart or favorites.

### Mobile

Use a compact top bar with logo, a full-width or clearly expanding Search control, and cart. Search should take one tap to focus from every page. A bottom navigation may contain Home, Catalog, Search, Favorites, and Account; show cart in the top bar so it stays visually distinct and reachable. If five bottom destinations become cramped, Favorites moves into Account, but Search and Catalog do not.

The mobile Catalog control opens full-height navigation with clear back behavior, not a desktop mega-menu squeezed into a drawer. Persistent navigation respects safe areas and never obscures focused controls, validation messages, pagination, or a product-page purchase action.

## 2. Catalog navigation

### Four independent discovery axes

| Axis | Customer question | Primary destination |
| --- | --- | --- |
| Category | What type of product is it? | Category directory and category results |
| Producer | Who makes it and in which range? | Manufacturer → brand → series → products |
| Collection | Why is it grouped or relevant? | Editorial collection landing |
| Search | Do I know a name, SKU, or related concept? | Suggestions and full results |

These axes share the same catalog result system and can be combined as filters, but their directories and meaning remain distinct. A collection is not a category; a brand is not a manufacturer.

### Desktop Catalog menu

The Catalog trigger opens a bounded panel focused on categories:

- a short list of top-level categories in the first column;
- children of the focused/selected category in the main area, grouped only where taxonomy supports it;
- links to **All products** and the full **Category directory**;
- at most one quiet editorial link, which is labeled as a collection and never replaces navigation.

Keyboard focus enters the panel predictably; arrow keys may move between category tabs only if the complete tabs pattern is implemented. Hover can preview but is never the only way to choose. The panel does not enumerate all manufacturers, brands, series, or collections. Manufacturers and Collections remain direct header links.

### Mobile Catalog navigation

The first view offers four explicit choices: Categories, Manufacturers, Collections, and All products. Categories drill down one level at a time with a persistent “View all in …” action and a visible back/title row. The customer can open products from any level without traversing all descendants. Expansion state survives a return from results during the current navigation session.

### Directories and entity pages

The **Category directory** presents top-level categories with short descriptions and nested links. It supports trees deeper than two levels without showing the entire tree by default. Counts are optional and, if shown, include eligible descendant products and use consistent formatting.

The **Manufacturer directory** is a searchable index, not a product-result workaround. At useful scale it provides `A–Z`, `А–Я`, and `0–9/#` jump groups, plus an “All” reset. Disable or omit empty letters. Mixed-script names stay under their displayed first character; do not transliterate silently. Each row shows manufacturer name and, where authoritative, country and product count. Common or featured manufacturers may appear above the index in a small curated area, never as the only access path.

A **manufacturer page** contains name, concise description, relevant metadata, brand list, optional series overview, and its products. A **brand page** names and links its manufacturer, explains the brand briefly, exposes its series, and shows products. A **series page or section** is scoped to its brand, because series names are not globally unique. Customers can choose “All products” at every producer level; no intermediate click is mandatory.

### Breadcrumbs

Breadcrumbs use canonical hierarchy, not the customer’s full click history:

- `Home / Categories / Chocolate / Dark chocolate`;
- `Home / Manufacturers / Manufacturer / Brand / Series`;
- `Home / Collections / Collection name`;
- `Home / Product name` for a product with optional linked category context below the title.

On mobile, retain Home only for assistive technology if space is scarce and show the immediate parent plus current location. Breadcrumb links remain real links and do not encode conflicting filters. Back/forward restores query, filters, sort, page, density, and an appropriate scroll position.

## 3. Catalog page

Catalog state is shareable in the URL. Category, manufacturer, brand, series, collection, query, price, stock, tags, sorting, and page are explicit. Compatible selections across axes can coexist; incompatible child selections are identified and cleared with an explanation rather than silently broadening the query.

### Desktop composition

Order the page as follows:

1. breadcrumbs;
2. title and exact result count;
3. optional short category/entity description;
4. active filter chips with per-chip removal and **Clear all**;
5. toolbar with filter visibility, sort, and Comfortable/Compact density;
6. filter rail plus results;
7. pagination and result-range summary.

The left filter rail is sticky within the viewport but independently scrolls only when necessary. Filter updates keep the title and controls stable. Apply inexpensive discrete filters immediately; debounce typed price fields and do not request on every keystroke. Announce updated result counts without moving focus or automatically scrolling to the top.

Immediately visible filters are:

- current category and its nearby children;
- in stock;
- price range;
- manufacturer, or brand when already within a manufacturer;
- active filters, even if their group would otherwise be hidden.

Behind **More filters** are brand in the general catalog, series, tags/characteristics, sale/new flags, collection, and long-tail facets. A high-use facet may be promoted only from evidence. Long manufacturer/brand/series groups show a small initial set plus in-filter search and **Show more**; do not render thousands of checkboxes. Counts follow the catalog ADR’s disjunctive semantics, and selected zero-result values remain visible.

Sorting includes Popular, New, Price low to high, Price high to low, Rating if rating data is trustworthy, Name, and Relevance when a query exists. Do not imply editorial or relevance ranking where the API cannot supply it. The chosen sort and density persist for the session or as a local preference.

**Comfortable** density uses the browsing card. **Compact** density uses rows on desktop and laptop, optimized for order building. Density is a labeled control, not icon-only. Pagination remains the baseline for deterministic navigation; show Previous/Next, nearby page numbers, and “items X–Y of N.” Do not use infinite scroll as the sole navigation model. Returning from a product restores the prior page and scroll anchor.

### Mobile composition

Keep title and result count above a sticky compact toolbar. The toolbar has two equal, comfortably tappable controls: **Filters · N** and **Sort**, plus a density choice in the Filters sheet or an adjacent labeled menu. An always-visible count or colored dot makes active filters unmistakable; active chips scroll horizontally beneath the toolbar and can be removed individually.

Filters open as an accessible full-height drawer or bottom sheet with:

- a title and active count;
- collapsible groups;
- local search for long values;
- **Clear all** and a sticky **Show N products** action;
- safe-area and on-screen-keyboard accommodation.

Changes are staged until Apply/Show products on mobile, so the list does not rerender behind the sheet. Closing without applying preserves the prior result state. Sorting is a short radio list and applies immediately after selection. Touch targets are at least approximately 44 by 44 CSS pixels unless a platform convention offers equivalent usability.

### Loading, errors, and empty results

On first load, skeletons approximate the selected card density without fake text. During refetch, retain existing results with a subtle updating indicator and disable only controls that would cause conflicting state. Pagination changes place focus on the results heading and scroll there unless the navigation is a history restoration.

An error keeps filters and the last safe result state where possible and offers Retry. An empty result states that no products match, lists the active constraints, and offers Clear all plus one-step removal of likely restrictive filters. A genuinely empty category or collection uses editorial guidance instead of pretending filters caused it.

## 4. Product presentations

Both presentations share one product identity and cart state. Adding a product changes the button into an inline quantity control; it must not open a modal or move the customer away from the list. Quantity controls use the product’s minimum and step, display pending state locally, serialize rapid changes per line, and reconcile to authoritative server results.

### A. Comfortable browsing card

Default information hierarchy:

1. product image with restrained sale/new badge when applicable;
2. manufacturer or brand as a quiet eyebrow;
3. product name, allowed two lines;
4. weight/unit and concise stock state;
5. current price, then old price when meaningful;
6. Add to cart or inline quantity control;
7. favorite toggle.

SKU, series, detailed stock number, minimum, and step are secondary. Show series alongside the producer only when it differentiates the item. Reveal SKU in an accessible secondary line or card detail affordance; do not rely on hover. Show minimum/step near the add control when either differs from one, for example “min 6 · step 3.” Ratings appear only if supported by trustworthy data. The entire title/image area links to the product, while quantity and favorite remain separate controls.

### B. Compact high-density row

The desktop row uses stable columns: small thumbnail; name/SKU; manufacturer–brand–series; unit/weight; availability; unit price; quantity; line subtotal; favorite. Allow the customer to hide the thumbnail locally for maximum density. The name and quantity remain visible at narrower laptop widths; less important producer fields collapse to one secondary line.

SKU is prominent enough for known-item scanning. Stock uses short text plus a semantic indicator, never color alone. The numeric quantity input supports direct replacement, step buttons, keyboard arrows, select-on-focus where safe, Enter to commit, and Escape to restore the last confirmed value. The line subtotal appears after a nonzero quantity. Inline errors sit with the row and do not shift unrelated rows excessively.

On mobile, “compact” becomes a restrained two-line list item rather than a desktop table squeezed horizontally: thumbnail/name/price first, producer/SKU/stock second, and a full-width quantity control. Horizontal scrolling is not required to buy.

## 5. Global search UX

Search is available from every storefront page. `Cmd+K` on macOS and `Ctrl+K` elsewhere focuses or opens it; `/` may be added only if it does not interfere with text entry. Escape closes suggestions and returns focus to the trigger. On mobile, tapping Search opens a dedicated full-height search surface with immediate keyboard focus.

Use an accessible combobox/listbox interaction. Suggestions begin after a short, documented minimum query; debounce requests, cancel obsolete requests, and ignore late responses. Each result has a canonical destination and works even when the product has never appeared in the current catalog page.

Suggestion groups appear only when populated, in this order for general text:

1. Products;
2. Categories;
3. Manufacturers and brands;
4. Series;
5. Collections.

Ranking within the panel comes from the search contract: exact SKU first, then strong product-name intent, then entity and exploratory matches. Preserve per-type quotas so products cannot crowd out navigation. Show a small type label, primary name, and only one useful secondary line—SKU/producer for products, parent for series/brand, and “Collection” for editorial results. Highlight matched text with typographic weight, not bright marker color, and never inject server-provided HTML.

Up/Down moves the active option, Enter opens it, Escape closes, and Tab follows normal focus order. The active result is visually distinct and announced through `aria-activedescendant`; result count and loading changes use restrained live announcements. Pointer and keyboard selection behave identically.

When the field is empty, show at most five local recent searches with individual remove and Clear recent controls; do not show account-sensitive history on a shared server-rendered page. A lightweight loading row replaces neither the typed query nor the whole panel. Empty suggestions say “No suggestions” and retain **Search for “…”**. Enter or the final panel action opens `/search?q=…`, where the full catalog layout supports facets, sort, density, pagination, corrected-query guidance when supported, and the same URL/back behavior.

## 6. Product page

The product is a full canonical page, not a modal. Above the fold on desktop use a two-column layout: gallery on the left and purchasing information on the right. On mobile, use gallery, identity, price/stock, and purchasing controls in that order; a sticky purchase bar is allowed only when it does not duplicate or obscure errors and quantity rules.

The gallery has one dominant image, thumbnail navigation when multiple real images exist, descriptive alternative text, and optional zoom that works without trapping keyboard or touch users. Do not fabricate multiple images or let placeholders overpower product information.

Information hierarchy:

- linked manufacturer, brand, and series lineage;
- product name;
- SKU with a copy action if useful;
- weight/unit;
- current price and old price;
- clear stock state;
- minimum and step immediately beside the quantity control;
- quantity control, primary Add to cart action, and secondary Favorite toggle.

After addition, retain the customer on the page, show the confirmed cart quantity, and offer a quiet cart link. State explicitly that cart placement does not reserve stock where that message is relevant. Stock/minimum/step errors appear inline and preserve the attempted value for correction.

Below the purchase area, use a readable description and a definition-list or table of product attributes. Hide absent attributes rather than showing empty fields. Follow with focused links to the producer, series, category, and relevant published collection. Related products are one bounded section based on an explainable relationship such as same series or category; do not surround the page with unrelated carousels or promotional cards.

## 7. Collections

The **Collections directory** lists published editorial selections with title, short premise, seasonal status where applicable, one representative image, and product count when authoritative. It can group current seasonal collections separately from evergreen collections, but expired or draft collections do not appear publicly.

A **collection landing page** contains breadcrumbs, title, restrained editorial image, concise introduction explaining why the products belong together, and optional editorial notes. The product area then uses the standard catalog presentation, filters, density, sort, and pagination. Default ordering may be editorial when supported, with normal alternatives available.

Collections never appear as category children and do not define product type. A seasonal collection is the same editorial content type with a publication window, not a permanent taxonomy node. Direct links to unavailable collections follow the agreed publication policy (intentional not-found, archive, or redirect); the UI must not guess from dates alone.

## 8. Large-order principles

The following rules apply across catalog, search results, favorites, repeat review, and cart:

- **Density is explicit.** Comfortable is the discovery default; Compact is a persistent local preference for work lists.
- **Quantity editing is immediate and resilient.** Direct numeric entry, correct min/step behavior, per-line pending state, serialized updates, and inline recovery replace confirmation dialogs.
- **Keyboard flow is continuous.** Tab order follows rows; Enter commits a quantity; optional shortcuts are documented and never replace standard controls. After a successful edit, focus stays on that product.
- **Scroll position is preserved.** Adding, favoriting, filtering, returning from a product, and resolving a line error do not reset the list. Stable keys and anchors restore position after pagination/history navigation.
- **Feedback is predictable.** Show pending, confirmed, and failed state at the affected row; use a non-blocking summary announcement for assistive technology. Never show success until the server confirms it. Undo is suitable for a confirmed removable list action when the API supports safe recovery, not for purchase confirmation.
- **Modals are exceptional.** Do not use one modal per product or for long review flows. Use pages for product detail, cart, checkout review, and large repeat review; use drawers for bounded filters and small menus.
- **Batch actions have explicit scope.** Labels distinguish selected positions, visible page, current group, all filtered matches, and entire list. Show the affected count before execution. An empty selection never means “all.” Partial success, if a contract supports it, is presented per line.
- **Selection survives view changes.** Filtering, sorting, collapsing groups, pagination, and density changes do not silently alter persisted checkout selection or temporary bulk selection. Summaries distinguish positions from total units and disclose hidden selected positions.
- **Performance does not erase usability.** Use bounded reads and stable rows. Virtualize only after profiling, with browser find, focus, screen-reader access, and position restoration preserved.

The cart should inherit compact rows, local search, issue filters, collapsible manufacturer groups, exact scope labels, authoritative totals, and sticky summary actions. Its detailed selection, revision, checkout, and repeat-order rules remain governed by ADR 005 and are outside this document’s redesign scope.

## 9. Responsive behavior

Breakpoints are chosen where content stops fitting, not as device-brand targets.

| Range | Header and navigation | Catalog and cards | Product page |
| --- | --- | --- | --- |
| Wide desktop | Full peer navigation, always-visible wide search, labeled utilities | Filter rail plus four or more comfortable columns; compact rows retain all key columns | Spacious two-column gallery/purchase layout; attributes can use two columns |
| Laptop | Shorter labels or account menu consolidation; search remains visible | Filter rail remains if results have adequate width; three comfortable columns; compact producer metadata condenses | Two columns with smaller gallery; purchase area remains readable and sticky only if it fits viewport |
| Tablet | Compact top header; search may occupy its own row; touch-friendly navigation panel | Filter rail becomes drawer; two or three comfortable columns; compact mode uses simplified rows | Two columns only when each remains usable; otherwise stacked with purchase details before long content |
| Mobile | Logo/search/cart top bar plus bottom navigation; full-height catalog/search surfaces | Sticky Filters/Sort controls; two browsing cards only when names and controls remain legible, otherwise one; compact two-line items | Single column; swipeable gallery, inline rules/errors, optional safe-area-aware purchase bar |

Layout changes must not reset search text, filters, quantities, selection, focus intent, or scroll anchor. Desktop-only hover affordances gain visible touch alternatives. Drawers become inline controls when enough space returns, and any open overlay closes safely with focus restored.

## 10. Visual language

### Typography

Choose a contemporary, highly legible type system without committing to a package. Use a restrained display scale for home/editorial headings and a compact UI scale for catalog work. Product names use medium emphasis; price and primary actions use stronger weight; metadata uses size and tone, not extreme light weights. Dense rows must remain readable at normal zoom and with longer Russian names. Use tabular numerals for prices, quantities, SKUs where appropriate, and aligned totals.

### Spacing and density

Base discovery layouts on a consistent spacing rhythm with generous page margins and clear section separation. Do not express every group as a card. Compact lists reduce vertical padding and image size, not text legibility or control targets. Related label/value pairs stay close; unrelated sections gain more space.

### Corners, borders, and shadows

Use small-to-medium radii consistently: modest on inputs, buttons, thumbnails, and panels; larger radii only for a few editorial surfaces. Pills are reserved for tags, counts, and removable filters. Prefer quiet warm-gray borders and surface changes. Shadows indicate elevation for overlays or sticky layers, not decoration on every product.

### Color

Use an off-white or parchment base, white/cream surfaces, charcoal or deep brown-green text, and muted secondary text. Choose one restrained accent—such as moss, olive, or mineral green—for primary action and selection, with verified contrast. Use semantic colors consistently for available/success, limited/warning, unavailable/error, and information; always pair color with text or icon. Pink, bright lime, orange, and gradients are not the default brand language.

### Images and motion

Real, SKU-specific product photography should be the most visually expressive element. Use consistent crops, neutral backgrounds, responsive sizes, and stable aspect ratios to avoid layout shift. Decorative imagery is secondary and must not obscure navigation or text.

Motion is brief and functional: menu/drawer transition, focus/selection feedback, and quantity confirmation. Avoid parallax, looping decoration, card hopping, and large route transitions. Respect reduced-motion preferences and never make animation necessary to understand state.

## 11. Interaction and accessibility

Target WCAG 2.2 AA in implementation and verification.

- All navigation, filters, cards, quantity controls, pagination, menus, drawers, dialogs, and galleries work by keyboard without requiring hover.
- Every interactive element has a clearly visible focus indicator with sufficient contrast. Custom checkboxes and numeric controls retain equivalent focus, name, role, value, and state.
- Use semantic links for navigation and buttons for actions. Headings form a meaningful outline; landmarks and a working skip link allow bypassing repeated navigation.
- Dialogs and drawers have an accessible name, constrained focus, Escape close when safe, background inertness, scroll containment, and focus restoration to the invoking control. Do not place long product or repeat-order workflows in dialogs.
- Search follows the accessible combobox pattern. Tabs, disclosure controls, and galleries implement their complete keyboard pattern rather than roles alone.
- Product images have meaningful alt text; decorative images have empty alt. Prices, old prices, stock, and quantity rules are understandable to screen readers without visual position or color.
- Dynamic result counts, cart confirmations, and errors use concise live announcements. Do not announce every intermediate keystroke or loading frame.
- Honor reduced motion in CSS and scripted scrolling. History restoration may restore position without smooth animation.
- Aim for touch targets of at least approximately 44 by 44 CSS pixels with adequate separation; dense desktop layouts may use visually smaller controls only when their interactive area and keyboard usability remain sufficient.
- Put validation next to the responsible field or row, summarize multi-error submissions at the top with links, preserve entered values, and move focus only when submission requires correction. Error copy states what happened and how to recover.
- Initial loading uses layout-stable skeletons; background loading preserves prior content; every failure has an actionable retry where retry is meaningful. Empty states distinguish no data, no matches, unavailable content, and signed-out access.

## 12. Page inventory

| Route | Responsibility |
| --- | --- |
| `/` | Storefront introduction, selected categories, current collections, and a restrained product sample |
| `/catalog` | All-product browsing with category and cross-axis filters |
| `/categories` | Full category directory |
| `/categories/:slug` | Category context, descendants, and filtered products |
| `/manufacturers` | Searchable manufacturer directory with A–Z/А–Я navigation |
| `/manufacturers/:slug` | Manufacturer identity, brands/series, and products |
| `/brands/:slug` | Brand identity, parent manufacturer, series, and products |
| `/brands/:brandSlug/series/:seriesSlug` | Brand-scoped series context and products |
| `/collections` | Published editorial and seasonal collection directory |
| `/collections/:slug` | Editorial introduction and collection products |
| `/search?q=…` | Full cross-catalog search results with facets and density |
| `/products/:slug` | Canonical product detail and purchase controls |
| `/favorites` | Saved products with search, grouping, density, and bounded batch actions |
| `/cart` | Authoritative cart work list and order summary; detailed behavior follows ADR 005 |
| `/checkout` | Future explicit checkout review when supported by real contracts |
| `/account` | Customer account entry and real supported profile functions only |
| `/account/orders` | Paged order history summaries |
| `/account/orders/:id` | Historical order detail from snapshots |
| `/account/orders/:id/repeat` | Full repeat-order review for large orders |
| `/delivery` | Real delivery/help policy content when confirmed |

Authentication may preserve an intended destination and return the customer after sign-in. Unsupported account, delivery, payment, loyalty, promotion, or B2B routes are not created as decorative placeholders.

## 13. UX implementation priorities

These are product/UX priorities, not a technical roadmap.

### P0 — foundation required for the first frontend slice

- visual tokens for type, spacing, color, focus, density, and restrained surfaces;
- global responsive header with first-class Search, Catalog, Manufacturers, Collections, account/favorites, and cart access;
- canonical links, breadcrumbs, route/history restoration, and intentional loading/error/empty patterns;
- accessible primitives for buttons, fields, quantity controls, disclosures, combobox, drawer/dialog, pagination, and feedback;
- comfortable card and compact row with authoritative cart/favorite states, minimum/step rules, and local pending/error behavior;
- canonical product page and one catalog/result surface using real data;
- baseline keyboard, screen-reader, reduced-motion, mobile, long-name, and large-list acceptance checks.

### P1 — important storefront UX

- category and manufacturer directories, including A–Z/А–Я navigation;
- manufacturer, brand, series, and collection landing pages;
- typed grouped global suggestions and full search results;
- complete catalog facets, active chips, sorting, density preference, pagination, and URL restoration;
- mobile catalog/search navigation and staged filter drawer;
- favorites and cart inheriting compact density, local search, grouping, explicit batch scope, and preserved selection;
- full-page order history/detail and repeat-order review consistent with ADR 005.

### P2 — enhancements

- evidence-led filter personalization or promotion of frequently used facets;
- optional thumbnail-free ultra-compact rows;
- recently viewed products or recent searches beyond local search history, only with an agreed privacy/retention policy;
- richer product gallery/zoom and explainable related-product improvements;
- measured search assistance such as typo recovery and query suggestions;
- virtualization only if profiling shows it is needed and accessibility can be preserved.

## Unresolved product decisions

The following decisions require product or operational input and must not be guessed during UI implementation:

- supported delivery regions, promises, thresholds, and whether location belongs in the global header;
- guest cart, guest checkout, sign-in timing, and reviewed guest-to-account merge behavior;
- authoritative maximum order size and expected typical/peak catalog, cart, favorites, and repeat-order sizes;
- whether prices can include fractional rubles and how unit versus package pricing must be labeled;
- approved stock vocabulary and whether exact stock counts are customer-visible;
- which product attributes and dietary/allergen facts are authoritative and filterable;
- collection expiry behavior and ownership of editorial content/order;
- manufacturer country, ratings, popularity, sale/new, and related-product data quality;
- mobile bottom-navigation composition if Favorites and Account cannot both fit comfortably;
- whether density preference is per device, per account, or session-only;
- real checkout, payment, address, and delivery contracts. Promo codes, loyalty, bonuses, multiple warehouses, and B2B accounts are not requirements unless separately approved.
