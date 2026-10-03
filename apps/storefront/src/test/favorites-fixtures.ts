import type { Favorites } from "@/lib/api/favorites";
import type { Product } from "@/lib/api/product-dto";

export function favoriteProduct(id = "p01", overrides: Partial<Product> = {}): Product {
  return {
    id, slug: `chocolate-${id}`, sku: `SKU-${id}`, name: `Шоколад ${id}`, description: "Настоящий товар",
    priceMinor: 48900, currency: "RUB", unit: "100 г", image: "", gallery: [],
    category: { id: "cat", slug: "chocolate", name: "Шоколад" },
    manufacturer: { id: "maker", slug: "maker", name: "Мастерская", country: "Россия" },
    brand: { id: "brand", slug: "brand", name: "Марка", manufacturerId: "maker", manufacturer: "Мастерская" },
    tags: [], badges: [], stock: 10, minimumQuantity: 1, quantityStep: 1, ...overrides,
  };
}

export function favoriteResponse(...items: Product[]): Favorites {
  const groups: Favorites["groups"] = [];
  for (const product of items) {
    let group = groups.find((g) => g.manufacturer.id === product.manufacturer.id);
    if (!group) { group = { manufacturer: product.manufacturer, products: [] }; groups.push(group); }
    group.products.push(product);
  }
  return { items, groups, total: items.length };
}

export function sessionResponse(userID = "A", csrfToken = `csrf-${userID}`) {
  return { authenticated: true, user: { id: userID, name: "Покупатель", email: "user@example.test" }, csrfToken, demoLogin: false };
}

export function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
