import { afterEach, describe, expect, it, vi } from "vitest";

import type { ProductApiError } from "./product";
import { getProduct, parseProduct } from "./product";

const productResponse = {
  id: "p01",
  slug: "dark-chocolate-sea-salt",
  sku: "CHA-701",
  name: "Тёмный шоколад 70% с морской солью",
  description: "Выразительный вкус.",
  priceMinor: 48900,
  oldPriceMinor: 54900,
  currency: "RUB",
  unit: "плитка 90 г",
  weightGrams: 90,
  image: "/assets/product-chocolate.webp",
  gallery: ["/assets/product-chocolate.webp"],
  category: { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад" },
  manufacturer: {
    id: "m-choco",
    slug: "choco-atelier",
    name: "Choco Atelier",
    country: "Россия",
  },
  brand: {
    id: "b-choco",
    slug: "choco-atelier",
    name: "Choco Atelier",
    manufacturerId: "m-choco",
    manufacturer: "Choco Atelier",
  },
  tags: ["веган"],
  rating: 4.9,
  reviewsCount: 142,
  stock: 24,
  minimumQuantity: 2,
  quantityStep: 2,
  createdAt: "2026-01-10T00:00:00Z",
};

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("product API boundary", () => {
  it("parses and maps the authoritative response without retaining unrelated fields", () => {
    const product = parseProduct(productResponse);

    expect(product).toMatchObject({
      id: "p01",
      minimumQuantity: 2,
      quantityStep: 2,
      manufacturer: { name: "Choco Atelier" },
    });
    expect(product.tags).toEqual(["веган"]);
    expect(product).not.toHaveProperty("rating");
  });

  it("accepts omitted optional fields and maps optional arrays to empty arrays", () => {
    const product = parseProduct({
      ...productResponse,
      gallery: undefined,
      tags: undefined,
      oldPriceMinor: undefined,
      weightGrams: undefined,
    });

    expect(product.gallery).toEqual([]);
    expect(product.tags).toEqual([]);
    expect(product.oldPriceMinor).toBeUndefined();
  });

  it("uses a no-store request and returns a typed product", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify(productResponse), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    const product = await getProduct("dark-chocolate-sea-salt", { fetcher });

    expect(product.id).toBe("p01");
    expect(fetcher).toHaveBeenCalledWith(
      "http://127.0.0.1:8080/api/products/dark-chocolate-sea-salt",
      expect.objectContaining({ cache: "no-store", signal: expect.any(AbortSignal) }),
    );
  });

  it("distinguishes not found from an unavailable backend", async () => {
    const notFoundFetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "not_found", message: "missing", requestId: "req-1" } }),
        { headers: { "Content-Type": "application/json" }, status: 404 },
      ),
    );
    const unavailableFetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "internal_error", message: "failed", requestId: "req-2" } }),
        { headers: { "Content-Type": "application/json" }, status: 500 },
      ),
    );

    await expect(getProduct("missing", { fetcher: notFoundFetcher })).rejects.toMatchObject({
      kind: "not-found",
      requestId: "req-1",
    } satisfies Partial<ProductApiError>);
    await expect(getProduct("broken", { fetcher: unavailableFetcher })).rejects.toMatchObject({
      kind: "unavailable",
      requestId: "req-2",
    } satisfies Partial<ProductApiError>);
  });

  it("rejects a malformed successful response", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(
      new Response(JSON.stringify({ ...productResponse, priceMinor: "48900" }), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );

    await expect(getProduct("bad", { fetcher })).rejects.toMatchObject({
      kind: "invalid-response",
    } satisfies Partial<ProductApiError>);
  });
});
