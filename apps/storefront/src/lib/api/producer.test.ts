import { describe, expect, it, vi } from "vitest";

import type { ProducerApiError } from "./producer";
import { getManufacturerPageData, getProducerDirectory } from "./producer";

const manufacturer = {
  id: "m-choco",
  slug: "choco-atelier",
  name: "Choco Atelier",
  country: "Россия",
  description: "Bean-to-bar шоколад",
  productCount: 4,
};

const brand = {
  id: "b-noir",
  slug: "atelier-noir",
  name: "Atelier Noir",
  manufacturerId: "m-choco",
  manufacturer: "Choco Atelier",
  description: "Тёмный шоколад",
  productCount: 2,
};

describe("producer API boundary", () => {
  it("maps real manufacturer and brand metadata", async () => {
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const body = String(input).endsWith("/api/manufacturers")
        ? { items: [manufacturer] }
        : { items: [brand] };
      return new Response(JSON.stringify(body), { status: 200 });
    });

    const result = await getProducerDirectory({ fetcher });

    expect(result.manufacturers[0]).toEqual(manufacturer);
    expect(result.brands[0]).toEqual(brand);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store" }));
  });

  it("resolves a manufacturer ID to its canonical slug and keeps brand ownership scoped", async () => {
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const url = String(input);
      if (url.endsWith("/api/manufacturers")) {
        return new Response(JSON.stringify({ items: [manufacturer] }), { status: 200 });
      }
      if (url.endsWith("/api/brands")) {
        return new Response(JSON.stringify({ items: [brand, { ...brand, id: "other", manufacturerId: "other" }] }), { status: 200 });
      }
      throw new Error(`unexpected producer request: ${url}`);
    });

    const result = await getManufacturerPageData("m-choco", { fetcher });

    expect(result?.manufacturer.slug).toBe("choco-atelier");
    expect(result?.brands).toHaveLength(1);
    expect(result?.brands[0]).toEqual(brand);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("uses two directory requests regardless of the number of manufacturer brands", async () => {
    const manyBrands = Array.from({ length: 40 }, (_, index) => ({
      ...brand,
      id: `brand-${index}`,
      slug: `brand-${index}`,
      name: `Brand ${index}`,
    }));
    const fetcher = vi.fn<typeof fetch>(async (input) => {
      const url = String(input);
      if (url.endsWith("/api/manufacturers")) {
        return new Response(JSON.stringify({ items: [manufacturer] }), { status: 200 });
      }
      if (url.endsWith("/api/brands")) {
        return new Response(JSON.stringify({ items: manyBrands }), { status: 200 });
      }
      throw new Error(`unexpected producer request: ${url}`);
    });

    const result = await getManufacturerPageData("choco-atelier", { fetcher });

    expect(result?.brands).toHaveLength(40);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls.map(([input]) => String(input))).toEqual(expect.arrayContaining([
      expect.stringMatching(/\/api\/manufacturers$/),
      expect.stringMatching(/\/api\/brands$/),
    ]));
  });

  it("returns not found only after a successful directory response", async () => {
    const fetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ items: [] }), { status: 200 }),
    );

    await expect(getManufacturerPageData("missing", { fetcher })).resolves.toBeUndefined();
  });

  it("keeps backend failure distinct from not found", async () => {
    const fetcher = vi.fn<typeof fetch>(async () =>
      new Response(JSON.stringify({ error: { requestId: "req-producer" } }), { status: 503 }),
    );

    await expect(getManufacturerPageData("missing", { fetcher })).rejects.toMatchObject({
      kind: "unavailable",
      requestId: "req-producer",
      status: 503,
    } satisfies Partial<ProducerApiError>);
  });
});
