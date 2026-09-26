export const CATALOG_SORTS = ["name", "new", "price_asc", "price_desc"] as const;
export const CATALOG_PAGE_SIZES = [24, 48, 96] as const;

export type CatalogSort = (typeof CATALOG_SORTS)[number];
export type CatalogView = "comfortable" | "compact";

export type CatalogSearchParams = Record<
  string,
  string | string[] | undefined
>;

export type CatalogState = {
  query?: string;
  category?: string;
  manufacturer?: string;
  brand?: string;
  series?: string;
  tags: string[];
  minPriceMinor?: number;
  maxPriceMinor?: number;
  inStock: boolean;
  onSale: boolean;
  newOnly: boolean;
  sort: CatalogSort;
  page: number;
  pageSize: (typeof CATALOG_PAGE_SIZES)[number];
  view: CatalogView;
};

function first(value: string | string[] | undefined): string | undefined {
  return Array.isArray(value) ? value[0] : value;
}

function text(value: string | string[] | undefined, maximum: number): string | undefined {
  const normalized = first(value)?.trim();
  if (!normalized) return undefined;
  return Array.from(normalized).slice(0, maximum).join("");
}

function positiveInteger(
  value: string | string[] | undefined,
  fallback: number,
  maximum = Number.MAX_SAFE_INTEGER,
): number {
  const raw = first(value);
  if (!raw || !/^\d+$/.test(raw)) return fallback;
  const parsed = Number(raw);
  return Number.isSafeInteger(parsed) && parsed > 0 && parsed <= maximum
    ? parsed
    : fallback;
}

function enabled(value: string | string[] | undefined): boolean {
  const normalized = first(value)?.toLowerCase();
  return normalized === "1" || normalized === "true";
}

function priceMinor(value: string | string[] | undefined): number | undefined {
  const raw = first(value)?.trim().replace(",", ".");
  if (!raw || !/^\d+(?:\.\d{1,2})?$/.test(raw)) return undefined;
  const [rubles, kopecks = ""] = raw.split(".");
  const result = Number(rubles) * 100 + Number(kopecks.padEnd(2, "0"));
  return Number.isSafeInteger(result) && result > 0 ? result : undefined;
}

export function formatCatalogPrice(amountMinor: number | undefined): string {
  if (!amountMinor) return "";
  const rubles = Math.floor(amountMinor / 100);
  const kopecks = amountMinor % 100;
  return kopecks ? `${rubles}.${String(kopecks).padStart(2, "0")}` : String(rubles);
}

function tags(value: string | string[] | undefined): string[] {
  const values = Array.isArray(value) ? value : value ? [value] : [];
  return Array.from(
    new Set(values.map((item) => item.trim()).filter(Boolean)),
  );
}

export function parseCatalogState(params: CatalogSearchParams): CatalogState {
  const sortValue = first(params.sort);
  const sort: CatalogSort = CATALOG_SORTS.includes(sortValue as CatalogSort)
    ? (sortValue as CatalogSort)
    : "name";
  const requestedSize = positiveInteger(params.size, 24, 100);
  const pageSize = CATALOG_PAGE_SIZES.includes(
    requestedSize as CatalogState["pageSize"],
  )
    ? (requestedSize as CatalogState["pageSize"])
    : 24;
  let minPriceMinor = priceMinor(params.min);
  let maxPriceMinor = priceMinor(params.max);
  if (minPriceMinor && maxPriceMinor && minPriceMinor > maxPriceMinor) {
    [minPriceMinor, maxPriceMinor] = [maxPriceMinor, minPriceMinor];
  }

  return {
    query: text(params.q, 200),
    category: text(params.category, 200),
    manufacturer: text(params.manufacturer, 200),
    brand: text(params.brand, 200),
    series: text(params.series, 200),
    tags: tags(params.tag),
    minPriceMinor,
    maxPriceMinor,
    inStock: enabled(params.stock),
    onSale: enabled(params.sale),
    newOnly: enabled(params.new),
    sort,
    page: positiveInteger(params.page, 1, 1_000_000),
    pageSize,
    view: first(params.view) === "compact" ? "compact" : "comfortable",
  };
}

export type CatalogStatePatch = Partial<CatalogState>;

export function catalogUrl(
  state: CatalogState,
  patch: CatalogStatePatch = {},
  options: { resetPage?: boolean } = {},
): string {
  const next = { ...state, ...patch };
  if (options.resetPage !== false && patch.page === undefined) next.page = 1;
  const params = new URLSearchParams();
  if (next.query) params.set("q", next.query);
  if (next.category) params.set("category", next.category);
  if (next.manufacturer) params.set("manufacturer", next.manufacturer);
  if (next.brand) params.set("brand", next.brand);
  if (next.series) params.set("series", next.series);
  for (const tag of next.tags) params.append("tag", tag);
  if (next.minPriceMinor) params.set("min", formatCatalogPrice(next.minPriceMinor));
  if (next.maxPriceMinor) params.set("max", formatCatalogPrice(next.maxPriceMinor));
  if (next.inStock) params.set("stock", "1");
  if (next.onSale) params.set("sale", "1");
  if (next.newOnly) params.set("new", "1");
  if (next.sort !== "name") params.set("sort", next.sort);
  if (next.page > 1) params.set("page", String(next.page));
  if (next.pageSize !== 24) params.set("size", String(next.pageSize));
  if (next.view === "compact") params.set("view", "compact");
  const query = params.toString();
  return query ? `/catalog?${query}` : "/catalog";
}

export function catalogHiddenFields(
  state: CatalogState,
  excluded: Array<keyof CatalogState> = [],
): Array<[string, string]> {
  const excludedSet = new Set(excluded);
  const entries: Array<[string, string]> = [];
  if (state.query && !excludedSet.has("query")) entries.push(["q", state.query]);
  if (state.category && !excludedSet.has("category")) entries.push(["category", state.category]);
  if (state.manufacturer && !excludedSet.has("manufacturer")) entries.push(["manufacturer", state.manufacturer]);
  if (state.brand && !excludedSet.has("brand")) entries.push(["brand", state.brand]);
  if (state.series && !excludedSet.has("series")) entries.push(["series", state.series]);
  if (!excludedSet.has("tags")) {
    for (const tag of state.tags) entries.push(["tag", tag]);
  }
  if (state.minPriceMinor && !excludedSet.has("minPriceMinor")) entries.push(["min", formatCatalogPrice(state.minPriceMinor)]);
  if (state.maxPriceMinor && !excludedSet.has("maxPriceMinor")) entries.push(["max", formatCatalogPrice(state.maxPriceMinor)]);
  if (state.inStock && !excludedSet.has("inStock")) entries.push(["stock", "1"]);
  if (state.onSale && !excludedSet.has("onSale")) entries.push(["sale", "1"]);
  if (state.newOnly && !excludedSet.has("newOnly")) entries.push(["new", "1"]);
  if (state.sort !== "name" && !excludedSet.has("sort")) entries.push(["sort", state.sort]);
  if (state.pageSize !== 24 && !excludedSet.has("pageSize")) entries.push(["size", String(state.pageSize)]);
  if (state.view === "compact" && !excludedSet.has("view")) entries.push(["view", "compact"]);
  return entries;
}
