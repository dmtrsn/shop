import { catalogUrl, parseCatalogState } from "./catalog-state";

const migrated: Record<string, string> = {
  home: "/", favorites: "/favorites", cart: "/cart",
  orders: "/account/orders", profile: "/account",
};

// Product/checkout/repeat were overlays, not hash routes.
export function mapLegacyHash(hash: string): string | null {
  if (!hash.startsWith("#/") || hash.length > 2048) return null;
  try {
    const decoded = decodeURIComponent(hash);
    if ([...decoded].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127 || char === "\\")
      || /%[0-9a-f]{2}/i.test(decoded)) return null;
  } catch { return null; }
  const [route, query = "", ...extra] = hash.slice(2).split("?");
  if (extra.length || route.includes("#") || query.includes("#")) return null;
  if (route === "") return "/";
  if (Object.hasOwn(migrated, route)) return migrated[route];
  // Next owns sign-in. Old hashes carry no returnTo, so /login falls back to home.
  if (route === "login") return "/login";
  // Demo delivery/payment/bonus claims are not migrated.
  if (route === "delivery") return "/";
  if (route !== "catalog") return null;
  const old = new URLSearchParams(query);
  const params: Record<string, string> = {};
  for (const key of ["q", "category", "manufacturer", "collection"]) {
    if (old.has(key)) params[key] = old.get(key)!;
  }
  params.stock = old.get("inStock") ?? "";
  params.sale = old.get("sale") ?? "";
  params.new = old.get("newOnly") ?? old.get("new") ?? "";
  params.min = old.get("minPrice") ?? "";
  params.max = old.get("maxPrice") ?? "";
  params.sort = ({ priceAsc: "price_asc", priceDesc: "price_desc", new: "new", name: "name" } as Record<string, string>)[old.get("sort") ?? ""] ?? "name";
  params.view = old.get("view") === "list" ? "compact" : "comfortable";
  params.page = old.get("page") ?? "";
  // Both use 24 items/page. "popular" alone has no Next equivalent.
  return catalogUrl(parseCatalogState(params), {}, { resetPage: false });
}
