import { describe, expect, it, vi } from "vitest";

import type { CollectionApiError } from "./collection";
import { getCollectionPageData, getCollections } from "./collection";

const collection = {
  id: "col-gifts",
  slug: "gifts-under-2000",
  name: "Подарки до 2 000 ₽",
  description: "Готовые наборы без долгого выбора",
  accent: "coral",
  image: "",
  productIds: ["p04", "p06", "p07"],
  productCount: 3,
  startsAt: "0001-01-01T00:00:00Z",
  endsAt: "0001-01-01T00:00:00Z",
};

function product(id: string) {
  return {
    id,
    slug: `product-${id}`,
    sku: `SKU-${id}`,
    name: `Товар ${id}`,
    description: "Описание",
    priceMinor: 50000,
    currency: "RUB",
    unit: "упаковка",
    image: "",
    category: { id: "cat", slug: "category", name: "Категория" },
    manufacturer: { id: "maker", slug: "maker", name: "Мануфактура", country: "Россия" },
    brand: { id: "brand", slug: "brand", name: "Бренд", manufacturerId: "maker", manufacturer: "Мануфактура" },
    tags: [],
    badges: [],
    rating: 0,
    reviewsCount: 0,
    stock: 2,
    minimumQuantity: 1,
    quantityStep: 1,
    createdAt: "2026-01-01T00:00:00Z",
  };
}

describe("collection API boundary", () => {
  it("maps the current collection contract without inventing status or dates", async () => {
    const fetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ items: [collection] }), { status: 200 }),
    );

    const result = await getCollections({ fetcher });

    expect(result[0]).toEqual({
      id: collection.id,
      slug: collection.slug,
      name: collection.name,
      description: collection.description,
      accent: collection.accent,
      productIds: collection.productIds,
      productCount: 3,
    });
    expect(result[0]).not.toHaveProperty("status");
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store" }));
  });

  it("resolves an ID to canonical slug data and preserves explicit product order", async () => {
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const url = String(input);
      if (url.endsWith("/api/collections")) {
        return new Response(JSON.stringify({ items: [collection] }), { status: 200 });
      }
      const id = decodeURIComponent(url.split("/").at(-1) ?? "");
      return new Response(JSON.stringify(product(id)), { status: 200 });
    });

    const result = await getCollectionPageData("col-gifts", { fetcher });

    expect(result?.collection.slug).toBe("gifts-under-2000");
    expect(result?.products.map((item) => item.id)).toEqual(["p04", "p06", "p07"]);
    expect(fetcher.mock.calls.map(([input]) => String(input))).toEqual([
      expect.stringMatching(/\/api\/collections$/),
      expect.stringMatching(/\/api\/products\/p04$/),
      expect.stringMatching(/\/api\/products\/p06$/),
      expect.stringMatching(/\/api\/products\/p07$/),
    ]);
  });

  it("returns undefined only after a successful directory response", async () => {
    const fetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ items: [] }), { status: 200 }),
    );

    await expect(getCollectionPageData("missing", { fetcher })).resolves.toBeUndefined();
  });

  it("maps a contract-permitted null productIds value to an empty collection", async () => {
    const fetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ items: [{ ...collection, productIds: null, productCount: 0 }] }), { status: 200 }),
    );

    await expect(getCollections({ fetcher })).resolves.toMatchObject([
      { slug: "gifts-under-2000", productIds: [], productCount: 0 },
    ]);
  });

  it("distinguishes malformed success data from backend failure", async () => {
    const malformed = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ items: [{ ...collection, productIds: "wrong" }] }), { status: 200 }),
    );
    const failed = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ error: { requestId: "req-collection" } }), { status: 503 }),
    );

    await expect(getCollections({ fetcher: malformed })).rejects.toMatchObject({
      kind: "invalid-response",
    } satisfies Partial<CollectionApiError>);
    await expect(getCollections({ fetcher: failed })).rejects.toMatchObject({
      kind: "unavailable",
      requestId: "req-collection",
      status: 503,
    } satisfies Partial<CollectionApiError>);
  });
});
