import { describe, expect, it } from "vitest";

import { parseProductQuery, productListHref } from "./product-query";

describe("product list URL state", () => {
  it("reads valid values and falls back on anything else", () => {
    expect(parseProductQuery({ q: " шоколад ", status: "archived", page: "3", limit: "25" })).toEqual({ q: "шоколад", status: "archived", page: 3, limit: 25 });
    expect(parseProductQuery({ status: "deleted", page: "-1", limit: "7" })).toEqual({ q: "", status: "active", page: 1, limit: 50 });
    expect(parseProductQuery({ page: "1.5", status: ["all", "active"] })).toEqual({ q: "", status: "all", page: 1, limit: 50 });
  });

  it("writes canonical URLs without defaults", () => {
    expect(productListHref({ q: "", status: "active", page: 1, limit: 50 })).toBe("/products");
    expect(productListHref({ q: "a&b", status: "all", page: 2, limit: 100 })).toBe("/products?q=a%26b&status=all&page=2&limit=100");
  });
});
