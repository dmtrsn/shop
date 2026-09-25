# Storefront visual specification

Status: Proposed. Date: 2026-09-25. Applies to the future React/Next.js customer storefront. The [storefront UX specification](storefront-ux-spec.md) remains the authority for behavior; this document defines visual hierarchy and composition.

## Non-negotiable hierarchy

- **Search is the highest-priority discovery control.** It is visually prominent and directly reachable at every responsive size.
- **Cart is the highest-priority transactional utility.** It remains distinct from account and favorites and exposes an unambiguous position count.
- Products—not UI ornament—provide most of the color, texture, and appetite appeal.
- “Popular” sorting or a “Popular products” section exists only when the backend supplies a documented, trustworthy popularity signal. The client must never infer popularity from array order, ratings, stock, fixture data, or local behavior.

The current storefront’s warm neutral foundation, dark typography, prominent search, clear product photography, and compact commerce controls are useful precedents. Its bright lime/orange emphasis, large radii, gradient promotional panel, repeated framed tiles, and product carousel treatment are not visual requirements for the replacement.

## 1. Visual design principles

1. **Let the shopping task attract attention first.** The primary scan path is Search → categories/current collection → products. On commerce pages it is product identity → price/stock → quantity/action. Decorative headlines, campaigns, and trust messages must not interrupt that path.
2. **Make product photography the richest layer.** UI surfaces stay warm, quiet, and low-chroma so packaging and confectionery colors remain distinctive. A page should still feel branded when all decorative imagery is removed.
3. **Use whitespace according to intent.** Give the homepage, collection introductions, and product storytelling room to breathe. Tighten spacing in filters, compact rows, cart, favorites, and repeat-order review where comparison speed matters.
4. **Create hierarchy with typography and alignment before containers.** Use headings, rules, spacing, columns, and background bands to group content. Add a card only when an item is independently actionable or needs a boundary.
5. **Express premium through restraint and precision.** Consistent baselines, strong photography, exact spacing, crisp type, quiet materials, and reliable interaction should feel considered without gold effects, formal luxury cues, or oversized editorial type.
6. **Reference confectionery through material and image, not cartoons.** Parchment-like neutrals, cocoa-dark text, subtle paper/ceramic warmth, and close product photography may carry the category character. Avoid candy icons, frosting shapes, novelty lettering, sprinkles, and sugary gradients.
7. **Keep color purposeful.** One natural accent identifies action and selection. Semantic colors communicate availability, warning, and error. Promotional content does not introduce a new color system for every campaign.
8. **Separate Browse and Build through density, not branding.** Comfortable browsing uses larger imagery and more vertical rhythm. Compact ordering uses aligned rows, tabular numbers, smaller thumbnails, and quiet separators while retaining the same typography, colors, and controls.
9. **Design for real content extremes.** Long Russian names, multi-word manufacturers, sale prices, minimum/step rules, large counts, missing images, and error messages fit without overlap, tiny type, or ellipsis hiding the only useful distinction.
10. **Every visual state communicates function.** Hover, focus, selected, pending, success, disabled, and error states remain distinguishable without motion or color alone and meet the intended WCAG 2.2 AA contrast target.

## 2. Color system

Define colors as semantic roles and tonal relationships. Exact values are selected and contrast-tested during design-system implementation.

| Role | Direction | Use |
| --- | --- | --- |
| Page background | Warm off-white, parchment, or light cream | Default canvas; enough warmth to avoid clinical white without tinting product photography |
| Primary surface | Near-white with a warm undertone | Inputs, menus, drawers, product detail surfaces, and content requiring separation |
| Secondary surface | Slightly deeper warm neutral | Selected rows, quiet grouped areas, image placeholders, and alternating content bands |
| Primary text | Charcoal or very deep warm green-brown | Headings, body text, prices, and primary icons |
| Secondary text | Muted warm gray with AA contrast at its size | Metadata, counts, helper copy, and inactive controls |
| Border | Warm gray, visibly distinct from both adjacent surfaces | Dividers, inputs, rows, and bounded cards |
| Strong border | Darker neutral | Hovered inputs, selected boundaries, and high-definition separators |
| Primary accent | Restrained moss, olive, or muted botanical green | Primary actions, selected navigation, focus support, and limited brand moments |
| Accent hover/pressed | Progressively darker or deeper versions of the same accent | Interaction feedback; do not switch hue |
| Selected state | Pale tint of the accent plus a strong text/border treatment | Active filter, selected row, and current navigation; never color alone |
| Success/available | Natural green distinct from the brand accent | Confirmed additions and available stock |
| Warning/limited | Muted amber or ochre | Limited stock and recoverable attention states |
| Error/unavailable | Deep muted red | Validation, failed actions, and unavailable stock |
| Information | Desaturated blue-gray or neutral accent tint | Non-critical system information |

Primary buttons may use a dark charcoal fill with light text or the restrained accent with contrast-tested dark text. Choose one as the default and use the other sparingly; do not alternate them arbitrarily between pages.

Do not use color as filler behind every section, product, or icon. Do not color-code categories, manufacturers, or brands unless that color is authoritative content. Avoid bright lime and orange as dominant brand colors, candy gradients, beige-on-beige text, and multiple competing campaign accents. Product badges use semantic or neutral treatments and remain few. Every text/control combination, including muted text, disabled context, focus rings, and text over photography, must be evaluated against WCAG 2.2 AA; use an image scrim only where text must overlap a photograph.

## 3. Typography

Use one highly legible family or a restrained two-family system selected later. The character should be contemporary and human, with open forms for Cyrillic, clear numerals, and no confectionery novelty styling. A display face, if used, is limited to editorial headings and must include a complete, high-quality Cyrillic set.

| Level | Character and use |
| --- | --- |
| Display | Controlled, compact headline for the homepage hero and major collection stories; no ultra-thin weights or theatrical scale |
| Page title | Strong sans-serif or compatible display style; shorter than display and readable beside controls/counts |
| Section heading | Clear, medium-to-bold, with modest negative tracking only at large sizes |
| Product name | Medium emphasis, sentence case, neutral rhythm; two or three lines in comfortable cards and fully readable in compact/detail contexts |
| Body | Comfortable reading size and line height; bounded line length for descriptions and editorial copy |
| Metadata | Smaller but never faint; manufacturer, brand, series, weight, stock helper text, and counts |
| Buttons | Medium or semibold, sentence case; labels describe actions rather than marketing slogans |
| Price | Strong weight and tabular numerals; prominent without exceeding the product name’s overall visual weight |
| Old price | Secondary tone, smaller, struck through clearly; never the only evidence of a discount |
| SKU | Compact neutral style, optionally monospaced only if the selected face improves scanning; preserve letter/number distinction |
| Quantity/totals | Tabular numerals with stable widths so edits do not make rows jump |

Long Russian product names wrap naturally rather than shrinking below the product text scale. Comfortable cards reserve a consistent name area for at least two lines; compact rows allow wrapping to a second line before truncating secondary lineage. Avoid all caps except short labels such as SKU or restrained eyebrow text. Use uppercase sparingly because wide tracking and long Cyrillic labels reduce scan speed.

## 4. Spacing and layout

Use a centered responsive container with a comfortable reading maximum and the ability for catalog grids to use more width on large screens. Avoid an indefinitely expanding layout: beyond the useful catalog width, increase outer margin rather than line length or card count. Full-bleed photography is exceptional; header, text, and controls align to the same container edges.

Page gutters should step down with available width: generous on wide screens, efficient on laptops/tablets, and protective rather than spacious on mobile. They must never make a two-column mobile card unreadably narrow. Maintain a small base spacing unit and a limited scale so related distances repeat predictably.

- **Homepage/editorial:** the most generous vertical rhythm. Major sections are separated by whitespace or an occasional subtle surface band, not repeated boxes. Text/image relationships can be asymmetric.
- **Catalog:** compact header-to-results rhythm. Filters, toolbar, and products align to a clear column grid; the product area receives substantially more width than the filter rail.
- **Product page:** generous gallery/purchase gap and readable description width. Keep price, stock, quantity, and action close enough to read as one purchase group.
- **Compact ordering:** smallest vertical rhythm, consistent row height where content permits, aligned numeric columns, and thin row dividers. Reduce padding before reducing type size or touch/focus area.

Labels sit close to their value or control; unrelated groups receive more space. In definition lists, labels form a consistent column and values align. Grids use stable column and gutter rhythms; avoid irregular masonry for product listings. Visually group content with shared alignment, section rules, headings, and proximity before adding backgrounds or enclosing borders.

## 5. Shape language

Radii are restrained and tied to scale rather than applied universally:

- buttons and inputs: small-to-medium radius, visibly rectangular rather than pill-shaped;
- product imagery: modest radius or square corners within a card, consistent across the catalog;
- cards and bounded panels: medium radius, smaller than the current legacy 20–30px editorial treatment;
- drawers and mobile sheets: radius only on exposed leading corners;
- centered modals: medium radius appropriate to their elevation;
- dropdowns and search panels: medium radius, no bubble-like silhouette;
- chips, tags, badges, counts, compact statuses, and removable filters: pills are allowed because their compact token-like meaning supports the shape.

Primary and secondary action buttons do not become capsules at normal sizes. Nested elements reduce or remove radius: a rounded card should not contain a rounded image panel, rounded metadata panel, and rounded action panel simultaneously. Product images may have a slightly smaller radius than their parent. Icon buttons may be circular only for established symbolic actions such as close or favorite; other icon buttons use a compact rounded square.

## 6. Borders, elevation, and surfaces

Use three separation methods in this order:

1. spacing and alignment;
2. subtle background contrast or a one-pixel border;
3. shadow only when an element actually floats above another layer.

Product cards can use a quiet boundary or no outer border when the grid and image blocks already establish structure. Compact rows use dividers, not individual shadows. Filter groups use whitespace and horizontal rules. Alternating surface bands may distinguish one editorial homepage section, but avoid zebra striping the entire store.

Shadows primarily belong to the sticky header after content scrolls beneath it, search suggestions, menus, drawers, modals, and a sticky purchase/action area. Default sticky controls may begin with a border and gain a shallow shadow only while overlapping content. Use one restrained shadow family with low opacity and broad softness; avoid glowing colored shadows and hover shadows that make every card jump forward.

## 7. Buttons and controls

### Hierarchy

- **Primary:** one principal action per local region, usually Add to cart, Show products, or Continue. Solid dark or accent fill, high-contrast label, modest radius.
- **Secondary:** neutral surface with border; for alternative actions such as Favorite text action, Back, or View collection.
- **Tertiary/text:** no container until hover/focus; for navigation, Clear, Show more, and low-risk inline actions.
- **Destructive:** muted red text/border by default and solid error fill only at the final destructive confirmation. Never visually compete with the primary purchase action before it is relevant.
- **Icon button:** familiar icon plus accessible name and tooltip where discovery is uncertain. Critical navigation and purchase actions retain text.

### States

Default controls have a clear boundary and label. Hover changes tone/border without large movement. Pressed state is visibly deeper and may use a very small inward transform. Focus uses a consistent high-contrast outer ring that remains visible against all surfaces. Disabled controls retain legible labels, lose elevation, and do not rely on opacity alone; adjacent text explains non-obvious causes. Loading keeps the control width stable, retains an action label where space allows, adds a small progress indicator, and prevents duplicate submission without presenting success prematurely.

### Quantity controls

The quantity control is a single compact group: decrement, numeric value/input, increment. Internal dividers and one outer boundary make it read as one control rather than three buttons. The numeric area is widest, uses tabular numerals, and does not change width with quantity. In dense rows, buttons may appear visually compact while preserving adequate hit area and keyboard focus. Minimum/step helper text sits directly below or adjacent; stock errors replace helper text without moving unrelated columns. Pending updates use a subtle progress cue within the group. Confirmed state may briefly tint the border/surface; errors use border, icon, and text, never a shaking animation.

## 8. Product imagery

Use real SKU-specific imagery whenever available. Images must not be tinted by the UI or placed on a different decorative scene per card.

- Comfortable catalog images use one consistent near-square or slightly landscape ratio selected from actual asset quality. Reserve dimensions before load.
- Product-page primary imagery may use a taller/larger consistent frame, while thumbnails use the same crop logic as the source gallery.
- Use `object-fit: contain` for packaging that must remain fully identifiable; use a controlled editorial crop only for contextual collection photography. Do not mix these modes unpredictably in one grid.
- Image backgrounds are neutral, consistent, and close to the photography’s real background. Do not put a colored blob, gradient, or pattern behind every SKU.
- Thumbnails are quiet and bordered only on hover/selection. The selected thumbnail uses accent plus a non-color indicator.
- Missing images use a restrained neutral field with product/category text or a simple generic package mark. Never invent a colorful pseudo-product that could be mistaken for the SKU.
- Multiple-image galleries show thumbnails where space permits and a small count on mobile. Zoom is optional and secondary to accurate, fast images.
- Compact rows use small square thumbnails; customers may hide them locally in an ultra-dense preference. Missing thumbnails do not create a visually loud placeholder.

Product imagery should normally be the strongest colorful element on catalog and product pages. Compress and size responsively, preserve aspect ratio, and avoid layout shift; implementation details remain outside this visual specification.

## 9. Product card visual specification

### Comfortable card

The card is a product presentation, not a dashboard tile. It uses a dominant image occupying roughly the upper half to three-fifths of the card, followed by an unboxed text/action area. The image may sit on a quiet neutral field. Outer border and radius are subtle; hover changes border or surface and does not lift the card dramatically.

Within the content area:

1. manufacturer or brand appears first as one short muted line; show both only when the distinction helps;
2. product name follows in medium weight, left-aligned, with a consistent two-line area;
3. weight/unit and concise stock appear as quieter metadata;
4. minimum/step appears near the buying control only when not one;
5. price anchors the bottom-left, with old price beside or above it at reduced emphasis;
6. Add to cart or the quantity control anchors the bottom-right or spans the lower row when labels require it.

Favorite is a quiet icon button at the image’s top-right with a solid neutral backing sufficient over photography. Sale/New badges use the opposite corner, no more than two visible badges, compact text, and no diagonal ribbons. Stock is text with a small semantic marker; it is not a large promotional badge. Cards in the same row align prices and actions despite different name lengths.

### Compact product row

The compact row is visually a work list, not a shrunken card. It uses a horizontal baseline, small optional thumbnail, left-aligned name/SKU, restrained producer lineage, stock, aligned price, quantity control, line subtotal, and favorite. Rows are separated by thin rules or a shared table boundary; there are no individual shadows or floating rounded containers. Tabular numeric columns align vertically across dozens of items.

Its image is secondary and its SKU, quantity, price, and stock are more prominent than in the comfortable card. Hover may tint the full row lightly. Selected/pending/error states affect the row edge or background without changing its height. On mobile it recomposes into a two-line list item plus a purchase row; it does not preserve desktop columns through horizontal scrolling.

## 10. Header

### Desktop and laptop

The main header is compact—approximately one normal control height plus balanced vertical padding—so it does not dominate catalog viewports. Add a utility strip only for confirmed, useful service information; it uses small text and disappears when there is no real content.

From left to right: logo, Catalog, Manufacturers, Collections, prominent Search, Favorites, Account, Cart. The logo is a confident wordmark/symbol but not so wide that search is sacrificed. Catalog is a labeled navigation trigger, not the primary filled call-to-action. Manufacturers and Collections are text links with clear hover/current states.

Search receives the largest flexible width, a quiet contrasting field, search icon, descriptive placeholder, and shortcut hint on wide screens. Its focus state is stronger than its idle border. Favorites and Account use icon plus a short label when space permits. Cart uses icon plus label and a position-count badge; it has the strongest utility emphasis, but does not compete with an in-page Add to cart button.

The header is sticky with an opaque or nearly opaque warm surface and bottom border. Backdrop blur is optional and never the primary way content remains readable. After scrolling, the header may reduce vertical padding and hide a utility strip, but logo, Search, and Cart do not disappear or change order. Elevation appears only while content passes beneath it.

Badges are small count markers, not bright promotional dots. Use the accent or a high-contrast neutral; reserve red for errors/urgent states. Display `99+` in the header when necessary and exact positions at the destination.

### Mobile

Do not squeeze desktop links into one row. Use a top row with logo and Cart, followed by or integrated with a full-width Search field that takes one tap to focus. Keep the total mobile header compact enough to show content immediately. If search collapses while scrolling down, retain a labeled search icon in the top row and restore the field on upward scroll/focus; never hide access behind a hamburger.

A bottom navigation may provide Home, Catalog, Search, Favorites, and Account. Use icon plus short text, a quiet active state, and safe-area padding. Cart stays in the top bar as the transactional priority. The catalog opens a full-height navigation surface with its own title/back controls, not a floating desktop mega-menu.

## 11. Homepage

The homepage is a concise route into shopping, not an exhaustive catalog or a wall of carousels. Recommended order:

1. header;
2. shopping-first hero;
3. category directory preview;
4. one current editorial/seasonal collection;
5. selected products with a documented selection basis;
6. manufacturer discovery;
7. optional practical/repeat-order entry when real and relevant;
8. footer.

Do not add a section merely to fill the page. On mobile, maintain the same narrative and shorten each preview rather than making every section horizontally scroll.

### Hero / introduction

**Purpose:** identify the store and start a shopping path within the first viewport.

**Content:** one concise heading, one sentence of useful positioning, primary **Open catalog**, secondary current collection link only when published, and a prominent search entry if the persistent header search is not already visually dominant. Use one strong photographic composition showing real products or confectionery with sufficient negative space. A single factual service message may appear only if authoritative.

**Visual weight:** important but bounded. On desktop it should occupy substantially less than a full viewport—enough that the next category section is visible or clearly begins. The heading should not exceed a few short lines. Avoid an oversized rounded container; an edge-aligned image/text split or restrained full-width image band is preferable.

**Desktop:** asymmetric two-column composition, with text/action around two-fifths and image around three-fifths, or a photograph with a protected text zone. Search/header remains visually competitive with the hero.

**Mobile:** stack concise text, actions, and an image with a shallower crop. Keep Open catalog and Search visible without scrolling past a tall image. No overlaid fine text on a busy crop.

### Category discovery

**Purpose:** answer “what are you looking for?” immediately after the hero.

**Content:** six to eight top-level categories, optional trustworthy counts, and a link to the complete category directory.

**Visual weight:** strong navigational section, quieter than the hero image and denser than collections.

**Desktop:** use a typographic directory with one or two featured photographic category entries rather than a uniform wall of identical tiles. For example, a two-column list of category names/descriptions alongside one category image, or a clean multi-column link grid divided by rules. Every item need not have an icon, colored background, or border.

**Mobile:** a vertical or two-column text-led list with generous tap rows. Use at most one editorial image. Avoid a swipe-only category rail; customers must see the available range and reach All categories.

### Current editorial or seasonal collection

**Purpose:** provide timely character and an editorial reason to browse without redefining taxonomy.

**Content:** one published collection with title, short premise, representative photograph, product count if authoritative, and View collection. A second small collection link is acceptable only when it does not dilute the feature.

**Visual weight:** the homepage’s richest branded moment after the hero. Use a distinct but restrained surface band or image/text split. Derive color from the core neutral system rather than creating a campaign gradient.

**Desktop:** wide image with adjacent text, alternating alignment from the hero if useful. Do not present three unequal giant rounded cards.

**Mobile:** image, title, premise, and action stack in source order. Keep the image ratio shallow enough that products follow soon. Expired/draft collections never appear.

### Selected products

**Purpose:** let customers evaluate and buy representative products without opening the entire catalog.

**Content:** a bounded set, typically four to eight products, with a visible and truthful basis such as New arrivals, Editor’s selection, or From the current collection. “Popular” is prohibited unless the backend exposes a documented trustworthy popularity signal.

**Visual weight:** product photography becomes dominant; section framing stays quiet.

**Desktop:** one complete grid row using Comfortable cards, with a clear View all destination. Do not auto-rotate or hide most products in a carousel.

**Mobile:** use a short two-column grid when cards remain legible, otherwise a single column. A manual horizontal rail is acceptable only for this bounded preview if it exposes position and a View all link; a normal grid is preferred.

### Manufacturer discovery

**Purpose:** expose the independent producer axis and lead to the searchable directory.

**Content:** a short curated or alphabetical sample of manufacturer names, optional country/product count where authoritative, and a prominent All manufacturers link with A–Z/А–Я cue.

**Visual weight:** restrained, typographic, and lower than products/collection. Names are navigation, not decorative logos.

**Desktop:** a ruled multi-column list or a compact index strip with four to eight names. If actual manufacturer logos later exist and meet quality rules, use them sparingly and at consistent optical size.

**Mobile:** simple tap rows with name and optional metadata. Do not render hundreds of names or a logo carousel; direct customers to the searchable directory.

### Optional practical entry

**Purpose:** help signed-in repeat customers resume real tasks, such as viewing orders or opening repeat-order review.

Show this only when backed by account state and real behavior. It is a compact, quiet band near the end of the page, not a promotional card grid. Do not expose sensitive order content in publicly cached markup. Omit it rather than inventing loyalty, bonuses, B2B, or delivery promises.

### Footer

Use a clear typographic footer with Catalog, Manufacturers, Collections, account/help destinations, and confirmed contact/legal information. One warm dark or slightly deeper neutral surface may close the page. Avoid a second oversized logo, decorative newsletter panel, or unsupported claims.

## 12. Catalog page visual composition

The UX specification defines catalog behavior. Visually, breadcrumbs are small and quiet; the page title is the strongest text; the result count sits near the title in secondary tone instead of becoming a badge. A short description is width-bounded.

Active filters form one compact row beneath the heading. Chips use neutral/selected tint and small remove icons; **Clear all** is a text action. Do not wrap every inactive filter value into a pill.

The filter rail is visibly subordinate to products: narrower, smaller headings, simple check rows, quiet counts, and section rules instead of cards. The toolbar aligns to the product column, uses a subtle sticky surface/border, and groups sort and labeled density controls without looking like an admin toolbar.

Comfortable mode uses photography-led cards and generous grid gaps. Compact mode switches to one ruled list with aligned columns, reduced image scale, tighter spacing, and tabular numeric data; it must look materially denser, not like a one-column stack of comfortable cards. Pagination is centered or aligned to the results column, with restrained square controls and a clear current page. Filters, pagination, and sticky tools never acquire more saturation or elevation than product actions.

“Popular” appears in sort options only when supplied by a documented trustworthy backend signal. If not, omit it rather than disabling it or deriving it in the client.

## 13. Product page visual composition

On wide screens, allocate approximately half to three-fifths of the main width to the gallery and the remainder to product information. The gallery is visually dominant but not full-bleed; its neutral frame and thumbnails stay quieter than the product. The purchase column begins with small linked lineage, then a large but controlled title, weight/unit and SKU, price, stock, quantity rules, controls, and actions.

Price is strong and close to stock/quantity. Stock uses short text plus a semantic marker. Minimum and step are helper information directly adjacent to the quantity control. Add to cart is the clear primary action; Favorite is secondary and may use icon plus label on the detail page. The group should look composed and calm—not a bright boxed conversion widget—and may become sticky only when its full content fits without trapping the customer.

Description and attributes start after a clear section break. Use a readable text column and a ruled definition list/table, not separate cards per fact. Manufacturer, brand, series, category, and relevant collection links form a concise related-navigation block. One bounded related-product grid follows; it does not compete with the purchase area or multiply into unrelated rails.

On mobile, gallery comes first, followed immediately by identity and purchase controls. A sticky bottom action, if used, contains only the essential confirmed price/action state, respects safe areas, and yields to the on-screen keyboard and inline errors.

## 14. Search surface

### Desktop suggestions

The search panel aligns exactly with the search field and is wide enough for names without becoming a mega-menu. Use a primary surface, fine border, real elevation, and medium radius. Keep internal padding tight. Groups are separated by whitespace and small muted labels, not individual containers.

Product suggestions may show a small neutral thumbnail, name, and one line of SKU/producer; entity results use a simple type icon or label rather than fake imagery. Matching text is semibold. Prices align at the edge only for product results. The keyboard-active row uses a pale accent/neutral surface plus a leading indicator or strong text so it is not color-only. The final Search for action is separated by one rule.

Loading uses one compact progress row or subtle indicator while keeping the query visible. Recent searches are plain rows with a history icon and an individually reachable remove action. No-result state uses one sentence and the full-search action, without illustration.

### Mobile search

Search becomes a full-screen surface below the system/top safe area. The top row has Back, an autofocus search field, and Clear when needed. Results scroll under a stable search row; the keyboard must not cover the active result or full-search action. Use larger row targets and fewer secondary details than desktop. Group order and active/focus styling stay consistent. Avoid a modal card floating over dimmed content—the surface is a mobile navigation destination.

## 15. Mobile visual language

Mobile recomposes priorities instead of scaling desktop:

- Search and Cart remain top-level; secondary desktop navigation moves to the bottom navigation or full-height Catalog surface.
- Page gutters become efficient, while vertical section gaps remain sufficient to preserve hierarchy.
- Display type steps down substantially; product names and prices do not shrink below comfortable reading sizes.
- Comfortable cards use two columns only when long Russian names, price, stock, and controls remain usable. Otherwise use one column or a media/list composition.
- Compact rows become stacked list items: image/name/price, metadata/stock, then quantity/action. Never force desktop tables into horizontal scrolling for purchase tasks.
- Filter, catalog, and search drawers use the full available height, strong title/back hierarchy, sticky Apply where applicable, and surface/border separation rather than heavy shadows.
- Sticky filters, bottom navigation, and purchase/cart actions coordinate so no more than one bottom action layer is present at a time.
- Apply `env(safe-area-inset-*)` spacing where relevant. When the on-screen keyboard opens, hide or reposition bottom navigation/sticky actions so inputs, suggestions, and validation remain visible.
- Touch targets remain approximately 44 by 44 CSS pixels even when the visible icon/control is smaller. Dense visual treatment does not mean cramped interaction.

## 16. Motion

Allowed motion is short, local, and informative:

- color, border, and opacity transitions for hover/focus/pressed state;
- a brief menu, drawer, or sheet entrance/exit that preserves spatial origin;
- subtle search-panel appearance;
- a restrained quantity/cart confirmation tint or icon change;
- small disclosure expansion when content position remains understandable;
- skeleton-to-content fade only when it does not delay reading.

Avoid page-transition spectacle, parallax, bouncing icons, looping decoration, automatically moving carousels, card lift/scale large enough to reflow or distract, and animated counters for ordinary updates. Typical control transitions should feel immediate; overlays may be slightly longer but never ornamental. Under `prefers-reduced-motion`, remove transforms and smooth scrolling, make overlays appear without travel, and preserve all state communication through static border, text, and icon changes.

## 17. Empty, loading, and error states

States inherit the page’s typography, container, and alignment. They should feel like part of the store, not a generic dashboard alert.

- **Empty:** use a short title, one explanatory sentence, and one relevant action. A small line icon or quiet product-placeholder mark is optional; no giant illustration. A filtered-empty catalog keeps chips and useful controls visible.
- **Initial loading:** reserve the final layout with low-contrast skeleton blocks matching image/text geometry. Do not simulate readable text or pulse aggressively.
- **Background loading:** retain content, show a small inline/sticky progress cue, and reduce only the affected control’s interactivity.
- **Inline error:** place muted error tint, icon, and concrete recovery text near the control/row. Preserve entered values and surrounding products.
- **Page error:** retain the header and context, use a bounded readable message and Retry. Do not replace the store with a technical error code; make a reference ID secondary if supplied.
- **Unavailable content:** distinguish removed/unpublished entity from zero matching products. Provide a valid parent route without inventing substitutions.

Success feedback is equally restrained: confirm near the changed control and through an accessible announcement. Toasts are compact, do not stack indefinitely, and never carry information available nowhere else.

## 18. Design anti-patterns: do not do this

- Do not put a card inside a card inside a rounded section container.
- Do not use oversized radii on every image, button, panel, and navigation item.
- Do not use gradients—especially candy pink, bright lime, or orange—as the storefront’s visual identity.
- Do not introduce five competing accent colors for categories, campaigns, or controls.
- Do not create low-contrast beige-on-beige text, borders, disabled states, or focus rings.
- Do not center every product name beneath an image; left alignment supports fast scanning and long Russian names.
- Do not use icon-only labels for Catalog, Search, Cart, Add to cart, filters, or unfamiliar critical actions.
- Do not hide Catalog or Search behind a hamburger menu on desktop.
- Do not combine categories, all manufacturers, brands, series, and collections into one giant mega-menu.
- Do not turn every homepage section into a horizontal carousel or every category into an identical colored tile.
- Do not show a full-viewport empty hero, auto-changing slides, promotional badge clusters, or decoration that competes with product imagery.
- Do not use glassmorphism/backdrop blur on every sticky or floating surface.
- Do not add shadows to every product card, row, input, or hover state.
- Do not squeeze desktop data tables or their full column set onto mobile.
- Do not use tiny controls to achieve dense layouts; reduce padding and imagery before hit areas or type.
- Do not show more than two promotional badges on a product or make stock status look like a promotion.
- Do not fabricate ratings, popularity, scarcity, exact stock, delivery promises, or discount urgency.
- Do not expose a “Popular” sort or homepage heading without a documented trustworthy backend popularity signal.
- Do not use client array order, fixture ratings, inventory level, or click history as an improvised popularity ranking.
- Do not use decorative product placeholders that can be mistaken for actual packaging.
- Do not make routine state changes depend on animation, hover, or color alone.
- Do not copy a generic SaaS dashboard vocabulary—stat cards, control-panel chrome, segmented pills everywhere—into the customer storefront.

## 19. Homepage wireframes

These diagrams communicate hierarchy and relative weight, not exact pixels.

### A. Desktop homepage

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│ LOGO  Catalog  Manufacturers  Collections  [ Search products…       ⌘K ]  ♡ Account  CART 3 │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  Concise store proposition           ┌────────────────────────────────────┐  │
│  One useful supporting sentence      │                                    │  │
│  [ Open catalog ]  Current collection│      ONE STRONG PRODUCT PHOTO      │  │
│                                      │                                    │  │
│                                      └────────────────────────────────────┘  │
├──────────────────────────────────────────────────────────────────────────────┤
│ CATEGORIES                                              All categories →     │
│ Chocolate           Biscuits            Sweets                                │
│ short description   short description   short description                   │
│ ───────────────────────────────────────────────────────────────────────────  │
│ Waffles             Marshmallow         Gifts                               │
├──────────────────────────────────────────────────────────────────────────────┤
│ ┌─────────────────────────────┐  CURRENT COLLECTION                          │
│ │   editorial/product photo   │  Short editorial premise                    │
│ │                             │  Product count · View collection →          │
│ └─────────────────────────────┘                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ SELECTED PRODUCTS · truthful basis                           View all →       │
│ [ product ]     [ product ]     [ product ]     [ product ]                  │
├──────────────────────────────────────────────────────────────────────────────┤
│ MANUFACTURERS                                          A–Z / А–Я directory → │
│ Name · country       Name · country       Name · country       Name · country│
├──────────────────────────────────────────────────────────────────────────────┤
│ [Optional compact signed-in repeat-order entry; otherwise omitted]           │
├──────────────────────────────────────────────────────────────────────────────┤
│ FOOTER: Catalog · Manufacturers · Collections · Account/Help · Legal         │
└──────────────────────────────────────────────────────────────────────────────┘
```

### B. Mobile homepage

```text
┌──────────────────────────────┐
│ LOGO                    CART 3│
│ [ Search products…         ] │
├──────────────────────────────┤
│ Concise store proposition    │
│ One useful sentence          │
│ [ Open catalog             ] │
│ Current collection →         │
│ ┌──────────────────────────┐ │
│ │   shallow product photo  │ │
│ └──────────────────────────┘ │
├──────────────────────────────┤
│ CATEGORIES             All → │
│ Chocolate        Biscuits    │
│ Sweets           Waffles     │
│ Marshmallow      Gifts       │
├──────────────────────────────┤
│ CURRENT COLLECTION           │
│ ┌──────────────────────────┐ │
│ │     editorial photo      │ │
│ └──────────────────────────┘ │
│ Title · short premise        │
│ View collection →            │
├──────────────────────────────┤
│ SELECTED PRODUCTS      All → │
│ [ product ] [ product ]      │
│ [ product ] [ product ]      │
├──────────────────────────────┤
│ MANUFACTURERS      A–Z/А–Я → │
│ Name · metadata              │
│ Name · metadata              │
│ Name · metadata              │
├──────────────────────────────┤
│ FOOTER                       │
├──────────────────────────────┤
│ Home  Catalog  Search  ♡  Me │
└──────────────────────────────┘
```

## 20. Future design-system token inventory

Define tokens before broad page implementation, but do not encode product rules inside visual tokens.

| Group | Needed roles |
| --- | --- |
| Color | canvas/surfaces, text, borders, accent scale, selection, semantic states, overlays, image scrims |
| Typography | families, display/title/heading/body/metadata/control styles, weights, line heights, tracking, tabular-number feature |
| Spacing | base unit and bounded scale for inline gaps, control padding, card padding, section rhythm, safe-area additions |
| Radius | compact control, input/button, card/panel, overlay, pill, circular icon action |
| Border | widths and semantic colors for default, strong, selected, error, divider |
| Elevation | sticky, dropdown, drawer, modal; no generic decorative card elevation |
| Motion | immediate/short/overlay durations, easing, opacity/transform limits, reduced-motion substitutions |
| Density | comfortable and compact row heights, image sizes, paddings, metadata gaps, control visual size |
| Container | reading width, storefront width, catalog-wide width, gutters, section insets |
| Grid | column counts, minimum card measure, gutters, filter/result proportions, product-page proportions |
| Breakpoint | content-driven transitions for full header, filter rail, card grid, product layout, mobile navigation |
| Z-index | base, sticky toolbar, header, dropdown, drawer, modal, toast; a short named scale rather than arbitrary values |
| Focus | ring color, width, offset, inset variant, high-contrast fallback |
| Icon | standard sizes, stroke weight, optical alignment, status/action variants |
| State | hover, pressed, selected, pending, disabled, success, warning, error surface/text/border combinations |
| Image | card/gallery/thumbnail ratios, neutral backgrounds, placeholder treatment, crop modes |

## Remaining visual and product decisions

Resolve these before final token values or high-fidelity implementation:

- final brand mark and whether a separate display typeface is justified by its Cyrillic quality and performance cost;
- exact accessible color values, whether the primary button is charcoal or moss, and the relationship between brand accent and semantic success;
- representative real product photography, packaging-background consistency, required card ratio, and image art direction;
- authoritative homepage hero message and whether a current published collection always exists;
- category imagery availability and which one or two category entries, if any, deserve photographic emphasis;
- the documented source for New, Editor’s selection, and especially Popular product sets;
- whether manufacturer logos exist in consistent quality or the directory remains typographic;
- exact density defaults by context and whether the optional thumbnail-free compact mode is warranted;
- mobile bottom-navigation membership and behavior when the keyboard or a sticky purchase action is present;
- confirmed delivery/contact/legal content for the utility bar and footer;
- localization requirements beyond Russian and their effect on type metrics, labels, and layout;
- browser/device support and measured content breakpoints used to turn conceptual responsive ranges into tokens.
