import { afterEach, describe, expect, it, vi } from "vitest";

import { parseCatalogState } from "../catalog-state";
import type { CatalogApiError } from "./catalog";
import {
  getCatalogData,
  parseCatalogPage,
  serializeCatalogRequest,
} from "./catalog";

export const catalogProductResponse = {
  id: "p01",
  slug: "dark-chocolate-sea-salt",
  sku: "CHA-701",
  name: "Тёмный шоколад 70% с морской солью",
  description: "Выразительный вкус.",
  priceMinor: 48900,
  oldPriceMinor: 54900,
  currency: "RUB",
  unit: "плитка 90 г",
  image: "/assets/product-chocolate.webp",
  category: { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад" },
  manufacturer: {
    id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", country: "Россия",
  },
  brand: {
    id: "b-choco", slug: "atelier-noir", name: "Atelier Noir",
    manufacturerId: "m-choco", manufacturer: "Choco Atelier",
  },
  series: "Origins",
  tags: ["веган"],
  badges: ["Новинка"],
  rating: 4.9,
  reviewsCount: 142,
  stock: 24,
  minimumQuantity: 1,
  quantityStep: 1,
  createdAt: "2026-01-10T00:00:00Z",
};

const facetsResponse = {
  categories: [{ id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад", count: 1 }],
  manufacturers: [{ id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", count: 1 }],
  brands: [{ id: "b-choco", slug: "atelier-noir", name: "Atelier Noir", count: 1 }],
  tags: [{ id: "веган", slug: "веган", name: "веган", count: 1 }],
  series: [{ id: "Origins", slug: "Origins", name: "Origins", count: 1 }],
  minPriceMinor: 48900,
  maxPriceMinor: 48900,
};

afterEach(() => vi.unstubAllGlobals());

describe("catalog API boundary", () => {
  it("maps a successful page and retains only catalog fields used by the storefront", () => {
    const page = parseCatalogPage({
      items: [catalogProductResponse], total: 1, page: 1, limit: 24, totalPages: 1,
    });

    expect(page.items[0]).toMatchObject({
      id: "p01", badges: ["Новинка"], manufacturer: { name: "Choco Atelier" },
    });
    expect(page.items[0]).not.toHaveProperty("rating");
  });

  it("serializes the real Go filter names and minor-unit prices", () => {
    const query = serializeCatalogRequest(parseCatalogState({
      q: "трюфель",
      category: "candy",
      manufacturer: "maker",
      brand: "brand",
      collection: "gifts-under-2000",
      series: "Noir",
      tag: ["орехи", "подарок"],
      min: "250.50",
      max: "900",
      stock: "1",
      sale: "1",
      new: "1",
      sort: "price_asc",
      page: "2",
      size: "48",
    }));

    expect(query).toContain("minPriceMinor=25050");
    expect(query).toContain("maxPriceMinor=90000");
    expect(query).toContain("inStock=true");
    expect(query).toContain("collection=gifts-under-2000");
    expect(query).toContain("newOnly=true");
    expect(query).toContain("tag=%D0%BE%D1%80%D0%B5%D1%85%D0%B8");
    expect(query).toContain("limit=48");
  });

  it("loads the paged result and current directory/facet data with no-store requests", async () => {
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const url = String(input);
      let body: unknown;
      if (url.includes("/api/catalog/facets")) body = facetsResponse;
      else if (url.includes("/api/catalog?")) body = { items: [catalogProductResponse], total: 1, page: 1, limit: 24, totalPages: 1 };
      else if (url.endsWith("/api/categories")) body = { items: [{ ...catalogProductResponse.category, productCount: 1 }] };
      else if (url.endsWith("/api/manufacturers")) body = { items: [{ ...catalogProductResponse.manufacturer, productCount: 1 }] };
      else body = { items: [{ ...catalogProductResponse.brand, productCount: 1 }] };
      return new Response(JSON.stringify(body), { status: 200 });
    });

    const result = await getCatalogData(parseCatalogState({}), { fetcher });

    expect(result.page.total).toBe(1);
    expect(result.brands[0].manufacturerId).toBe("m-choco");
    expect(fetcher).toHaveBeenCalledTimes(5);
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store", signal: expect.any(AbortSignal) }));
  });

  it("distinguishes malformed success data from backend failure", async () => {
    const invalidFetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ items: "wrong" }), { status: 200 }),
    );
    const failedFetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ error: { requestId: "req-catalog" } }), { status: 503 }),
    );

    await expect(getCatalogData(parseCatalogState({}), { fetcher: invalidFetcher })).rejects.toMatchObject({
      kind: "invalid-response",
    } satisfies Partial<CatalogApiError>);
    await expect(getCatalogData(parseCatalogState({}), { fetcher: failedFetcher })).rejects.toMatchObject({
      kind: "unavailable", requestId: "req-catalog", status: 503,
    } satisfies Partial<CatalogApiError>);
  });
});
