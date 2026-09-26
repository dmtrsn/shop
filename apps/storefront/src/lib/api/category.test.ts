import { describe, expect, it, vi } from "vitest";

import type { CategoryApiError } from "./category";
import { getCategories, getCategoryPageData } from "./category";

const directory = [
  { id: "cat-chocolate", slug: "chocolate", name: "Шоколад", description: "Плитки и драже", icon: "chocolate", productCount: 6 },
  { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад", parentId: "cat-chocolate", productCount: 4 },
  { id: "cat-dragee", slug: "dragee", name: "Драже в шоколаде", parentId: "cat-chocolate", productCount: 2 },
  { id: "cat-gifts", slug: "gift-boxes", name: "Подарочные наборы", productCount: 0 },
];

function directoryFetcher(body: unknown = { items: directory }, status = 200) {
  return vi.fn<typeof fetch>(async () => new Response(JSON.stringify(body), { status }));
}

describe("category API boundary", () => {
  it("maps real category metadata with one uncached request", async () => {
    const fetcher = directoryFetcher({ items: [...directory, { ...directory[3], id: "empty-parent", slug: "empty-parent", parentId: "" }] });

    const result = await getCategories({ fetcher });

    expect(result[0]).toEqual({
      id: "cat-chocolate",
      slug: "chocolate",
      name: "Шоколад",
      description: "Плитки и драже",
      parentId: undefined,
      productCount: 6,
    });
    expect(result[1].parentId).toBe("cat-chocolate");
    expect(result[4].parentId).toBeUndefined();
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(String(fetcher.mock.calls[0][0])).toMatch(/\/api\/categories$/);
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store" }));
  });

  it("resolves an ID and exposes ancestors, children, and siblings", async () => {
    const fetcher = directoryFetcher();

    const parent = await getCategoryPageData("cat-chocolate", { fetcher });
    expect(parent?.category.slug).toBe("chocolate");
    expect(parent?.ancestors).toEqual([]);
    expect(parent?.children.map((item) => item.slug)).toEqual(["chocolate-bars", "dragee"]);
    expect(parent?.siblings.map((item) => item.slug)).toEqual(["chocolate", "gift-boxes"]);

    const leaf = await getCategoryPageData("chocolate-bars", { fetcher });
    expect(leaf?.ancestors.map((item) => item.slug)).toEqual(["chocolate"]);
    expect(leaf?.parent?.slug).toBe("chocolate");
    expect(leaf?.children).toEqual([]);
    expect(leaf?.siblings.map((item) => item.slug)).toEqual(["chocolate-bars", "dragee"]);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("returns not found only after a successful directory response", async () => {
    await expect(getCategoryPageData("missing", { fetcher: directoryFetcher() })).resolves.toBeUndefined();
  });

  it("keeps backend failure distinct from not found", async () => {
    const fetcher = directoryFetcher({ error: { requestId: "req-category" } }, 503);

    await expect(getCategoryPageData("missing", { fetcher })).rejects.toMatchObject({
      name: "CategoryApiError",
      kind: "unavailable",
      requestId: "req-category",
      status: 503,
    } satisfies Partial<CategoryApiError>);
  });

  it("rejects a response that breaks the category contract", async () => {
    await expect(getCategories({ fetcher: directoryFetcher({ items: [{ id: "x", slug: "x", name: "X" }] }) }))
      .rejects.toMatchObject({ kind: "invalid-response" });
    await expect(getCategories({ fetcher: directoryFetcher({ categories: [] }) }))
      .rejects.toMatchObject({ kind: "invalid-response" });
  });
});
