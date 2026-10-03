import { describe, expect, it } from "vitest";

import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { favoriteProduct } from "@/test/favorites-fixtures";

import { parseCart } from "./cart";

function clone<T>(value: T): T { return JSON.parse(JSON.stringify(value)); }

describe("cart parser", () => {
  it("parses an empty cart with the Go zero timestamp", () => {
    const body = cartResponse();
    expect(body.updatedAt).toBe("0001-01-01T00:00:00Z");
    expect(parseCart(body)).toEqual(body);
  });

  it("keeps full items, manufacturer groups and server order", () => {
    const other = { id: "other", slug: "other", name: "Артель", country: "Россия" };
    const body = cartResponse(
      cartItem("p01"), cartItem("p02", 3, false),
      { ...cartItem("p03"), product: favoriteProduct("p03", { manufacturer: other }) },
    );
    body.updatedAt = "2026-10-01T12:30:00.123456Z";
    const cart = parseCart(body);
    expect(cart.items.map((line) => line.product.id)).toEqual(["p01", "p02", "p03"]);
    expect(cart.groups.map((group) => [group.manufacturer.id, group.items.map((line) => line.product.id)]))
      .toEqual([["maker", ["p01", "p02"]], ["other", ["p03"]]]);
    // Group items are the canonical item objects, not divergent copies.
    expect(cart.groups[0].items[1]).toBe(cart.items[1]);
    expect(cart.updatedAt).toBe("2026-10-01T12:30:00.123456Z");
  });

  it("retains Go-authoritative amounts, counts and issues without arithmetic", () => {
    const line = { ...cartItem("p01", 2), issue: "Доступно только 1", available: 1, lineTotalMinor: 77 };
    const body = cartResponse(line);
    Object.assign(body.summary, { subtotalMinor: 1, discountMinor: 2, deliveryMinor: 3, totalMinor: 12345, unavailableCount: 1 });
    body.groups[0].subtotalMinor = 999;
    const cart = parseCart(body);
    expect(cart.items[0]).toMatchObject({ issue: "Доступно только 1", available: 1, lineTotalMinor: 77 });
    expect(cart.summary).toEqual(body.summary);
    expect(cart.groups[0].subtotalMinor).toBe(999);
  });

  it.each([
    ["null", null],
    ["empty object", {}],
    ["missing items", { ...cartResponse(), items: null }],
    ["missing groups", { ...cartResponse(), groups: null }],
    ["missing summary", { ...cartResponse(), summary: null }],
    ["invalid timestamp", { ...cartResponse(), updatedAt: "yesterday" }],
    ["missing timestamp", { ...cartResponse(), updatedAt: undefined }],
    ["ungrouped item", { ...cartResponse(cartItem()), groups: [] }],
    ["duplicate item", cartResponse(cartItem(), cartItem())],
    ["empty summary", { ...cartResponse(), summary: {} }],
    ["wrong position count", (() => { const body = cartResponse(cartItem()); body.summary.positionCount = 2; return body; })()],
  ])("rejects a malformed aggregate: %s", (_name, body) => {
    expect(() => parseCart(body)).toThrow();
  });

  it.each(["quantity", "available", "lineTotalMinor", "selected", "issue"])("rejects a malformed %s", (field) => {
    const body = clone(cartResponse(cartItem()));
    (body.items[0] as unknown as Record<string, unknown>)[field] = {};
    expect(() => parseCart(body)).toThrow();
  });

  it("rejects a zero quantity line", () => {
    const body = clone(cartResponse(cartItem()));
    body.items[0].quantity = 0;
    body.groups[0].items[0].quantity = 0;
    expect(() => parseCart(body)).toThrow();
  });

  it.each([
    ["line total", (body: ReturnType<typeof cartResponse>) => { body.items[0].lineTotalMinor = 2 ** 53; }],
    ["product price", (body: ReturnType<typeof cartResponse>) => { body.items[0].product.priceMinor = 2 ** 53; }],
    ["summary total", (body: ReturnType<typeof cartResponse>) => { body.summary.totalMinor = Number.MAX_SAFE_INTEGER + 2; }],
    ["group subtotal", (body: ReturnType<typeof cartResponse>) => { body.groups[0].subtotalMinor = 1e300; }],
    ["fractional money", (body: ReturnType<typeof cartResponse>) => { body.summary.deliveryMinor = 0.5; }],
    ["negative discount", (body: ReturnType<typeof cartResponse>) => { body.summary.discountMinor = -1; }],
  ])("rejects unsafe %s instead of displaying corrupted money", (_name, corrupt) => {
    const body = clone(cartResponse(cartItem()));
    corrupt(body);
    expect(() => parseCart(body)).toThrow();
  });

  it("rejects groups that disagree with canonical items or manufacturer", () => {
    const changed = clone(cartResponse(cartItem()));
    changed.groups[0].items[0].quantity = 2;
    expect(() => parseCart(changed)).toThrow();
    const wrongMaker = clone(cartResponse(cartItem()));
    wrongMaker.groups[0].manufacturer.id = "other";
    expect(() => parseCart(wrongMaker)).toThrow();
  });
});
