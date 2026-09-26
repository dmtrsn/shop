import { describe, expect, it, vi } from "vitest";

import type { Category, Manufacturer } from "./catalog";
import {
  getHomePageData,
  selectFeaturedCollection,
  selectHomeCategories,
  selectHomeManufacturers,
} from "./home";

function category(id: string, overrides: Partial<Category> = {}) {
  return { id, slug: id, name: `Категория ${id}`, productCount: 1, ...overrides };
}

function manufacturer(id: string, name: string, productCount = 1) {
  return { id, slug: id, name, country: "Россия", productCount };
}

function collection(id: string, productIds: string[], overrides: Record<string, unknown> = {}) {
  return {
    id,
    slug: `slug-${id}`,
    name: `Подборка ${id}`,
    description: "",
    accent: "",
    productIds,
    productCount: productIds.length,
    startsAt: "0001-01-01T00:00:00Z",
    endsAt: "0001-01-01T00:00:00Z",
    ...overrides,
  };
}

function product(id: string) {
  return {
    id,
    slug: `product-${id}`,
    sku: `SKU-${id}`,
    name: `Товар ${id}`,
    description: "",
    priceMinor: 10000,
    currency: "RUB",
    unit: "шт.",
    image: "",
    category: { id: "cat", slug: "cat", name: "Категория" },
    manufacturer: { id: "m", slug: "m", name: "Производитель", country: "Россия" },
    brand: { id: "b", slug: "b", name: "Бренд", manufacturerId: "m", manufacturer: "Производитель" },
    tags: [],
    badges: [],
    rating: 0,
    reviewsCount: 0,
    stock: 1,
    minimumQuantity: 1,
    quantityStep: 1,
    createdAt: "2026-01-01T00:00:00Z",
  };
}

type Routes = Partial<Record<"categories" | "collections" | "manufacturers", unknown>> & {
  failing?: string[];
  missingProducts?: string[];
};

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status });
}

function backend(routes: Routes) {
  return vi.fn<typeof fetch>(async (input) => {
    const path = new URL(String(input)).pathname;
    if (routes.failing?.some((prefix) => path.startsWith(prefix))) {
      return json({ error: { code: "internal_error" } }, 500);
    }
    if (path === "/api/categories") return json({ items: routes.categories ?? [] });
    if (path === "/api/collections") return json({ items: routes.collections ?? [] });
    if (path === "/api/manufacturers") return json({ items: routes.manufacturers ?? [] });
    if (path.startsWith("/api/products/")) {
      const id = decodeURIComponent(path.split("/").at(-1) ?? "");
      if (routes.missingProducts?.includes(id)) return json({ error: { code: "not_found" } }, 404);
      return json(product(id));
    }
    throw new Error(`unexpected request ${path}`);
  });
}

function paths(fetcher: ReturnType<typeof backend>) {
  return fetcher.mock.calls.map(([input]) => new URL(String(input)).pathname).sort();
}

const seedLike: Routes = {
  categories: [category("chocolate"), category("bars", { parentId: "chocolate" }), category("candy")],
  collections: [collection("col-new", ["p02", "p03", "p13", "p22"]), collection("col-gifts", ["p04"])],
  manufacturers: [manufacturer("caffarel", "Caffarel"), manufacturer("kolomna", "Коломна")],
};

describe("homepage data", () => {
  it("uses exactly seven bounded requests and never the catalog", async () => {
    const fetcher = backend(seedLike);

    const data = await getHomePageData({ fetcher });

    expect(paths(fetcher)).toEqual([
      "/api/categories",
      "/api/collections",
      "/api/manufacturers",
      "/api/products/p02",
      "/api/products/p03",
      "/api/products/p13",
      "/api/products/p22",
    ]);
    expect(data.categories.status).toBe("ready");
    expect(data.manufacturers.status).toBe("ready");
    expect(data.collection).toMatchObject({
      status: "ready",
      data: {
        collection: { id: "col-new" },
        others: [{ id: "col-gifts" }],
      },
    });
  });

  it("requests only the first four editorial products and keeps productIds order", async () => {
    const fetcher = backend({
      collections: [collection("big", ["p9", "p1", "p7", "p3", "p5", "p2"])],
    });

    const data = await getHomePageData({ fetcher });

    expect(data.collection.status === "ready" && data.collection.data?.products.map((item) => item.id))
      .toEqual(["p9", "p1", "p7", "p3"]);
    expect(paths(fetcher).filter((path) => path.startsWith("/api/products/"))).toHaveLength(4);
  });

  it("isolates a failed categories request", async () => {
    const data = await getHomePageData({ fetcher: backend({ ...seedLike, failing: ["/api/categories"] }) });

    expect(data.categories.status).toBe("unavailable");
    expect(data.collection.status).toBe("ready");
    expect(data.manufacturers.status).toBe("ready");
  });

  it("does not request products when collections fail", async () => {
    const fetcher = backend({ ...seedLike, failing: ["/api/collections"] });

    const data = await getHomePageData({ fetcher });

    expect(data.collection.status).toBe("unavailable");
    expect(data.manufacturers.status).toBe("ready");
    expect(paths(fetcher).some((path) => path.startsWith("/api/products/"))).toBe(false);
  });

  it("marks the collection section unavailable when a referenced product is missing", async () => {
    const data = await getHomePageData({ fetcher: backend({ ...seedLike, missingProducts: ["p13"] }) });

    expect(data.collection.status).toBe("unavailable");
    expect(data.categories.status).toBe("ready");
  });

  it("resolves with every section unavailable when the backend is down", async () => {
    const fetcher = vi.fn<typeof fetch>(async () => {
      throw new TypeError("fetch failed");
    });

    await expect(getHomePageData({ fetcher })).resolves.toEqual({
      categories: { status: "unavailable" },
      collection: { status: "unavailable" },
      manufacturers: { status: "unavailable" },
    });
  });

  it("reports a successful response without a usable collection as ready and empty", async () => {
    const fetcher = backend({ ...seedLike, collections: [collection("empty", [])] });

    const data = await getHomePageData({ fetcher });

    expect(data.collection).toEqual({ status: "ready", data: undefined });
    expect(paths(fetcher).some((path) => path.startsWith("/api/products/"))).toBe(false);
  });
});

describe("homepage selection rules", () => {
  it("picks the first collection in response order that has products, without re-sorting", async () => {
    const fetcher = backend({
      collections: [
        collection("z-empty", [], { name: "Аааа пустая" }),
        collection("y-later", ["p1"], { name: "Яблоки" }),
        collection("a-first", ["p2"], { name: "Абрикосы" }),
      ],
    });
    const data = await getHomePageData({ fetcher });

    expect(data.collection.status === "ready" && data.collection.data?.collection.id).toBe("y-later");
  });

  it("does not re-apply collection schedules that Go already evaluated", async () => {
    const fetcher = backend({
      collections: [collection("past", ["p1"], { endsAt: "2000-01-01T00:00:00Z" })],
    });
    const data = await getHomePageData({ fetcher });

    expect(data.collection.status === "ready" && data.collection.data?.collection.id).toBe("past");
  });

  it("returns undefined when no collection has products", () => {
    expect(selectFeaturedCollection([])).toBeUndefined();
  });

  it("keeps root categories in API order, excludes children and caps at eight", () => {
    const categories: Category[] = [
      ...Array.from({ length: 10 }, (_, index) => category(`root-${index}`)),
      category("child", { parentId: "root-0" }),
    ];

    const result = selectHomeCategories(categories);

    expect(result.categories.map((item) => item.id)).toEqual(
      Array.from({ length: 8 }, (_, index) => `root-${index}`),
    );
    expect(result.totalRoots).toBe(10);
  });

  it("lists manufacturers with products alphabetically, Latin before Cyrillic, capped at eight", () => {
    const manufacturers: Manufacturer[] = [
      manufacturer("kolomna", "Коломна"),
      manufacturer("empty", "Aaa Empty", 0),
      manufacturer("sweet", "Sweetzerland"),
      manufacturer("belev", "Белёвские сладости"),
      manufacturer("caffarel", "Caffarel"),
      ...Array.from({ length: 6 }, (_, index) => manufacturer(`z${index}`, `Я ${index}`)),
    ];

    const result = selectHomeManufacturers(manufacturers).manufacturers.map((item) => item.id);

    expect(result).toEqual(["caffarel", "sweet", "belev", "kolomna", "z0", "z1", "z2", "z3"]);
    expect(result).not.toContain("empty");
  });
});
