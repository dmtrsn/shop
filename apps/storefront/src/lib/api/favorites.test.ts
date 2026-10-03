import { describe, expect, it } from "vitest";
import { favoriteProduct, favoriteResponse } from "@/test/favorites-fixtures";
import { parseFavorites } from "./favorites";

describe("favorites DTO", () => {
  it("parses full products in server order, including out-of-stock products", () => {
    const body = favoriteResponse(favoriteProduct("z", { stock: 0 }), favoriteProduct("a"));
    expect(parseFavorites(body)).toEqual(body);
    expect(parseFavorites(body).items.map((p) => p.id)).toEqual(["z", "a"]);
  });
  it("accepts empty arrays", () => {
    expect(parseFavorites({ items: [], groups: [], total: 0 })).toEqual(favoriteResponse());
  });
  it.each([
    null, { items: [], groups: [], total: -1 }, { items: null, groups: [], total: 0 },
    { items: [], groups: null, total: 0 }, { ...favoriteResponse(), total: 1 },
    { ...favoriteResponse(favoriteProduct()), groups: [] },
    favoriteResponse(favoriteProduct(), favoriteProduct()),
    favoriteResponse(favoriteProduct("", { slug: "" })),
    { ...favoriteResponse(favoriteProduct()), groups: [{ manufacturer: {}, products: [] }] },
    favoriteResponse(favoriteProduct("p", { priceMinor: 1.2 })),
  ])("rejects invalid or incomplete representations (%#)", (body) => {
    expect(() => parseFavorites(body)).toThrow();
  });
});
