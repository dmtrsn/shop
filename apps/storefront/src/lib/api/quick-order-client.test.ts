import { describe, expect, it, vi } from "vitest";
import { favoriteProduct } from "../../test/favorites-fixtures";
import { loadManufacturerProducts } from "./quick-order-client";

function response(page: number, pages: number, items: unknown[] = []) {
  return Response.json({ page, limit: 100, total: pages * 100, totalPages: pages, items });
}
function product(id: string, name: string, stock = 5) { return { ...favoriteProduct(id), name, stock }; }

describe("manufacturer product loader", () => {
  it.each([1, 2, 5, 7])("loads all %i pages, filters every request and sorts/deduplicates", async (pages) => {
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const url = new URL(String(input), "http://localhost");
      expect(url.pathname).toBe("/api/catalog");
      expect(Object.fromEntries(url.searchParams)).toEqual({ manufacturer: "m/1", sort: "name", limit: "100", page: url.searchParams.get("page") });
      const n = Number(url.searchParams.get("page"));
      return response(n, pages, n === 1 ? [product("z", "Яблоко", 0), product("a", "Абрикос")] : [product("z", "Яблоко"), product(`p${n}`, `Товар ${n}`)]);
    });
    const result = await loadManufacturerProducts("m/1", { fetcher });
    expect(fetcher).toHaveBeenCalledTimes(pages);
    expect(result.map((p) => p.id)).toEqual(["a", ...Array.from({ length: pages - 1 }, (_, i) => `p${i + 2}`), "z"]);
    expect(result.at(-1)?.stock).toBe(0);
  });
  it("bounds concurrency and rejects the entire load while aborting siblings", async () => {
    let active = 0; let peak = 0; let aborted = 0;
    const fetcher = vi.fn<typeof fetch>(async (input, init) => {
      const n = Number(new URL(String(input), "http://localhost").searchParams.get("page"));
      if (n === 1) return response(1, 8);
      active++; peak = Math.max(peak, active);
      if (n === 3) { active--; return new Response("{}", { status: 500 }); }
      return new Promise<Response>((_resolve, reject) => init?.signal?.addEventListener("abort", () => { active--; aborted++; reject(new DOMException("Aborted", "AbortError")); }, { once: true }));
    });
    await expect(loadManufacturerProducts("m", { fetcher })).rejects.toThrow();
    expect(peak).toBeLessThanOrEqual(4);
    expect(aborted).toBeGreaterThan(0);
  });
  it("propagates cancellation to every active request", async () => {
    const controller = new AbortController(); let signals = 0;
    const fetcher = vi.fn<typeof fetch>(async (input, init) => {
      const n = Number(new URL(String(input), "http://localhost").searchParams.get("page"));
      if (n === 1) return response(1, 5);
      signals++;
      if (signals === 4) queueMicrotask(() => controller.abort());
      return new Promise<Response>((_resolve, reject) => init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true }));
    });
    await expect(loadManufacturerProducts("m", { fetcher, signal: controller.signal })).rejects.toMatchObject({ name: "AbortError" });
    expect(signals).toBe(4);
  });
  it("follows a later increase in reported page count", async () => {
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const n = Number(new URL(String(input), "http://localhost").searchParams.get("page"));
      return response(n, n === 1 ? 2 : 3, [product(String(n), `Товар ${n}`)]);
    });
    expect((await loadManufacturerProducts("m", { fetcher })).map((p) => p.id)).toEqual(["1", "2", "3"]);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });
  it("rejects invalid products and pagination metadata", async () => {
    const badProduct = vi.fn<typeof fetch>(async () => response(1, 1, [{}]));
    await expect(loadManufacturerProducts("m", { fetcher: badProduct })).rejects.toThrow();
    const badMetadata = vi.fn<typeof fetch>(async () => Response.json({ page: 1, limit: 100, total: 200, totalPages: 1, items: [] }));
    await expect(loadManufacturerProducts("m", { fetcher: badMetadata })).rejects.toThrow("pagination");
  });
});
