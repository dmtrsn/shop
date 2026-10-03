import { isRecord } from "./json";
import { parseProduct, type Product } from "./product-dto";

export type Favorites = {
  items: Product[];
  groups: { manufacturer: Product["manufacturer"]; products: Product[] }[];
  total: number;
};

export function parseFavorites(body: unknown): Favorites {
  if (!isRecord(body) || !Array.isArray(body.items) || !Array.isArray(body.groups) ||
      !Number.isSafeInteger(body.total) || (body.total as number) < 0) {
    throw new Error("Invalid favorites response");
  }
  const items = body.items.map(parseProduct);
  const ids = new Set(items.map((product) => product.id));
  if (ids.size !== items.length || body.total !== items.length || items.some((p) => !p.id || !p.slug)) {
    throw new Error("Invalid favorites membership");
  }
  const grouped = new Set<string>();
  const manufacturers = new Set<string>();
  const groups = body.groups.map((group) => {
    if (!isRecord(group) || !isRecord(group.manufacturer) || !Array.isArray(group.products)) {
      throw new Error("Invalid favorites group");
    }
    const manufacturer = group.manufacturer;
    if (typeof manufacturer.id !== "string" || !manufacturer.id ||
        typeof manufacturer.slug !== "string" || typeof manufacturer.name !== "string" ||
        typeof manufacturer.country !== "string" || manufacturers.has(manufacturer.id)) {
      throw new Error("Invalid favorites manufacturer");
    }
    manufacturers.add(manufacturer.id);
    const products = group.products.map(parseProduct);
    for (const product of products) {
      if (!ids.has(product.id) || grouped.has(product.id) || product.manufacturer.id !== manufacturer.id) {
        throw new Error("Invalid favorites group membership");
      }
      grouped.add(product.id);
    }
    return {
      manufacturer: { id: manufacturer.id, slug: manufacturer.slug, name: manufacturer.name, country: manufacturer.country },
      products,
    };
  });
  if (grouped.size !== ids.size) throw new Error("Incomplete favorites groups");
  return { items, groups, total: items.length };
}
