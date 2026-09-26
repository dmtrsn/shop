import { describe, expect, it } from "vitest";

import { canonicalizeCatalogIdentity } from "./catalog-canonicalization";
import { catalogUrl, parseCatalogState } from "./catalog-state";

describe("catalog URL state", () => {
  it("parses supported values and normalizes invalid input", () => {
    const state = parseCatalogState({
      q: "  шоколад  ",
      category: "bars",
      min: "900",
      max: "500.50",
      page: "-4",
      size: "100",
      sort: "popular",
      stock: "yes",
      tag: ["веган", " веган ", "без сахара"],
      view: "table",
    });

    expect(state).toMatchObject({
      query: "шоколад",
      category: "bars",
      minPriceMinor: 50_050,
      maxPriceMinor: 90_000,
      page: 1,
      pageSize: 24,
      sort: "name",
      inStock: false,
      tags: ["веган", "без сахара"],
      view: "comfortable",
    });
  });

  it("serializes stable state and resets pagination for filter changes", () => {
    const state = parseCatalogState({
      manufacturer: "choco-atelier",
      page: "3",
      size: "48",
      sort: "price_desc",
      view: "compact",
    });

    expect(catalogUrl(state, { brand: "atelier-noir" })).toBe(
      "/catalog?manufacturer=choco-atelier&brand=atelier-noir&sort=price_desc&size=48&view=compact",
    );
    expect(catalogUrl(state, { page: 2 }, { resetPage: false })).toBe(
      "/catalog?manufacturer=choco-atelier&sort=price_desc&page=2&size=48&view=compact",
    );
  });

  it("server canonicalization removes an incompatible brand and its series", () => {
    const state = parseCatalogState({
      manufacturer: "north",
      brand: "atelier-noir",
      series: "Origins",
      page: "4",
    });
    const corrected = canonicalizeCatalogIdentity(state, {
      categories: [],
      manufacturers: [
        { id: "m-north", slug: "north", name: "Северная", country: "Россия", productCount: 3 },
      ],
      brands: [
        { id: "b-noir", slug: "atelier-noir", name: "Atelier Noir", manufacturerId: "m-choco", manufacturer: "Choco Atelier", productCount: 2 },
      ],
      producerSeries: [],
    });

    expect(corrected).toMatchObject({
      manufacturer: "north",
      brand: undefined,
      series: undefined,
      page: 4,
    });
  });

  it("server canonicalization rejects a series outside the selected brand", () => {
    const state = parseCatalogState({
      manufacturer: "choco-atelier",
      brand: "atelier-maison",
      series: "Origins",
    });
    const corrected = canonicalizeCatalogIdentity(state, {
      categories: [],
      manufacturers: [
        { id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", country: "Россия", productCount: 4 },
      ],
      brands: [
        { id: "b-maison", slug: "atelier-maison", name: "Atelier Maison", manufacturerId: "m-choco", manufacturer: "Choco Atelier", productCount: 2 },
      ],
      producerSeries: [
        { id: "Atelier Ganache", slug: "Atelier Ganache", name: "Atelier Ganache", count: 2 },
      ],
    });

    expect(corrected.brand).toBe("atelier-maison");
    expect(corrected.series).toBeUndefined();
  });
});
