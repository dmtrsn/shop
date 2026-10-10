import { describe, expect, it } from "vitest";
import { mapLegacyHash } from "./legacy-hash";

describe("actual legacy hash routes", () => {
  it.each([
    ["#/", "/"], ["#/home", "/"], ["#/catalog", "/catalog"], ["#/favorites", "/favorites"],
    ["#/cart", "/cart"], ["#/orders", "/account/orders"], ["#/profile", "/account"],
    ["#/login", "/login"], ["#/delivery", "/"],
  ])("maps %s to %s", (hash, path) => { expect(mapLegacyHash(hash)).toBe(path); });
  it.each(["", "#main-content", "#details", "#cart", "#/unknown", "#/product/p01", "#/orders/ord123",
    "#/checkout", "#/bonuses", "#/addresses", "#//evil.example", "#/catalog?q=%", "#/catalog?q=%0a", "#/cart\\evil",
    "#/catalog?x=1?y=2", "#/cart#extra"])("leaves unsupported/malformed fragment %s alone", (hash) => {
    expect(mapLegacyHash(hash)).toBeNull();
  });
  it("translates only compatible catalog filters, keeping ID values supported by Go", () => {
    expect(mapLegacyHash("#/catalog?q=milk&category=chocolate&manufacturer=m01&collection=col-new&inStock=true&sale=1&newOnly=true&minPrice=10.50&maxPrice=99&sort=priceDesc&view=list&page=3&returnTo=https://evil.example"))
      .toBe("/catalog?q=milk&category=chocolate&manufacturer=m01&collection=col-new&min=10.50&max=99&stock=1&sale=1&new=1&sort=price_desc&page=3&view=compact");
  });
  it("drops ignored private-route queries and uses Next's default for unsupported popularity order", () => {
    expect(mapLegacyHash("#/cart?returnTo=//evil.example")).toBe("/cart");
    expect(mapLegacyHash("#/catalog?sort=popular&view=grid")).toBe("/catalog");
  });
  it("drops obsolete hashes so mapped paths cannot retrigger the bridge", () => {
    expect(mapLegacyHash(new URL(mapLegacyHash("#/cart")!, "https://shop.test").hash)).toBeNull();
  });
});
