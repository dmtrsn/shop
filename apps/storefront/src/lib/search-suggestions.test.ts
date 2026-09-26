import { describe, expect, it, vi } from "vitest";

import {
  codePointLength,
  fetchSuggestions,
  parseSuggestions,
  searchResultsUrl,
  suggestionHref,
} from "./search-suggestions";

describe("search suggestion boundary", () => {
  it("parses known types and skips types the storefront cannot route yet", () => {
    const items = parseSuggestions({
      items: [
        { type: "product", id: "p01", slug: "dark-chocolate-sea-salt", title: "Тёмный шоколад", subtitle: "Choco Atelier · плитка 90 г", image: "/assets/p.webp" },
        { type: "category", id: "cat-bars", slug: "chocolate-bars", title: "Плиточный шоколад" },
        { type: "collection", id: "c1", slug: "winter", title: "Зима" },
      ],
    });

    expect(items).toEqual([
      { type: "product", id: "p01", slug: "dark-chocolate-sea-salt", title: "Тёмный шоколад", subtitle: "Choco Atelier · плитка 90 г", image: "/assets/p.webp" },
      { type: "category", id: "cat-bars", slug: "chocolate-bars", title: "Плиточный шоколад", subtitle: undefined, image: undefined },
    ]);
  });

  it("rejects malformed envelopes and known items", () => {
    expect(() => parseSuggestions([])).toThrow();
    expect(() => parseSuggestions({ items: [{ type: "product", id: "p01", title: "x" }] })).toThrow("slug");
    expect(() => parseSuggestions({ items: [{ type: "brand", id: "b", slug: "b", title: "B", subtitle: 1 }] })).toThrow("subtitle");
  });

  it("maps every suggestion type to its canonical destination", () => {
    const base = { id: "id", title: "t" };
    expect(suggestionHref({ ...base, type: "product", slug: "a b" })).toBe("/products/a%20b");
    expect(suggestionHref({ ...base, type: "category", slug: "chocolate" })).toBe("/categories/chocolate");
    expect(suggestionHref({ ...base, type: "manufacturer", slug: "choco-atelier" })).toBe("/manufacturers/choco-atelier");
    expect(suggestionHref({ ...base, type: "brand", slug: "atelier-noir" })).toBe("/catalog?brand=atelier-noir");
  });

  it("builds canonical full-results URLs from the trimmed query", () => {
    expect(searchResultsUrl("  шоколад ")).toBe(`/catalog?${new URLSearchParams({ q: "шоколад" })}`);
    expect(searchResultsUrl("   ")).toBe("/catalog");
  });

  it("counts Unicode code points like Go runes", () => {
    expect(codePointLength("шоколад")).toBe(7);
    expect(codePointLength("🍫🍫")).toBe(2);
  });

  it("requests the same-origin suggestion route with cancellation", async () => {
    const controller = new AbortController();
    const fetcher = vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ items: [] }) }) as Response);

    await expect(fetchSuggestions("тём ный", { fetcher, signal: controller.signal })).resolves.toEqual([]);
    expect(fetcher).toHaveBeenCalledWith(
      `/api/search/suggest?${new URLSearchParams({ q: "тём ный" })}`,
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("fails non-success responses so the UI can show its error state", async () => {
    const fetcher = vi.fn(async () => ({ ok: false, status: 502, json: async () => ({}) }) as Response);
    await expect(fetchSuggestions("шо", { fetcher })).rejects.toThrow("502");
  });
});
