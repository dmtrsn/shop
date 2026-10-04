import { describe, expect, it } from "vitest";

import { orderItem, orderResponse } from "@/test/order-fixtures";

import { parseOrder, parseOrderHistory } from "./order";

describe("Order parser", () => {
  it("keeps exactly Go's fields and server strings; zero deliveredAt means not delivered", () => {
    const order = parseOrder({ ...orderResponse({ comment: "  Домофон 12 " }), unexpected: "ignored" });
    expect(order).toEqual({
      id: "ord-2026-10484", number: "#10484", status: "processing", statusLabel: "Собираем заказ",
      items: [orderItem()], itemCount: 2, totalMinor: 126800, deliveryAddress: "Москва, Тестовая, 1",
      comment: "  Домофон 12 ", createdAt: "2026-10-03T12:00:00Z", deliveredAt: null,
    });
    expect(order).not.toHaveProperty("unexpected");
    expect(parseOrder(orderResponse({ deliveredAt: "2026-10-05T10:00:00Z" })).deliveredAt).toBe("2026-10-05T10:00:00Z");
    expect(parseOrder(orderResponse())).not.toHaveProperty("comment");
    expect(parseOrder(orderResponse({ items: [] })).items).toEqual([]);
  });

  it.each([
    ["not an object", null],
    ["null items (history-only shape)", orderResponse({ items: null })],
    ["missing id", orderResponse({ id: "" })],
    ["missing number", orderResponse({ number: undefined })],
    ["unsafe total", orderResponse({ totalMinor: Number.MAX_SAFE_INTEGER + 1 })],
    ["fractional total", orderResponse({ totalMinor: 10.5 })],
    ["negative total", orderResponse({ totalMinor: -1 })],
    ["string count", orderResponse({ itemCount: "2" })],
    ["bad createdAt", orderResponse({ createdAt: "yesterday" })],
    ["missing deliveredAt", orderResponse({ deliveredAt: undefined })],
    ["numeric comment", orderResponse({ comment: 5 })],
    ["zero quantity", orderResponse({ items: [{ ...orderItem(), quantity: 0 }] })],
    ["unsafe unit price", orderResponse({ items: [{ ...orderItem(), unitPriceMinor: 2 ** 60 }] })],
    ["missing product", orderResponse({ items: [{ ...orderItem(), productId: "" }] })],
    ["missing sku", orderResponse({ items: [{ ...orderItem(), sku: undefined }] })],
  ])("rejects malformed private data: %s", (_name, body) => {
    expect(() => parseOrder(body)).toThrow();
  });
});

describe("Order history parser", () => {
  it("parses full snapshots in server order, including empty history", () => {
    const first = orderResponse();
    const second = orderResponse({ id: "older", number: "#1" });
    expect(parseOrderHistory({ items: [first, second] })).toEqual([parseOrder(first), parseOrder(second)]);
    expect(parseOrderHistory({ items: [] })).toEqual([]);
  });

  it("normalizes null history items without losing units or weakening detail", () => {
    const order = orderResponse({ items: null, itemCount: 7 });
    expect(parseOrderHistory({ items: [order] })[0]).toMatchObject({ items: [], itemCount: 7 });
    expect(() => parseOrder(order)).toThrow();
    expect(parseOrderHistory({ items: [orderResponse({ items: [], itemCount: 0 })] })[0].itemCount).toBe(0);
  });

  it.each([
    null, {}, { items: null }, { items: [null] },
    { items: [orderResponse({ items: undefined })] },
    { items: [orderResponse({ number: "" })] },
    { items: [orderResponse({ createdAt: "bad" })] },
    { items: [orderResponse({ itemCount: Number.MAX_SAFE_INTEGER + 1 })] },
    { items: [orderResponse({ totalMinor: Number.MAX_SAFE_INTEGER + 1 })] },
    { items: [orderResponse({ items: [{ ...orderItem(), quantity: 1.5 }] })] },
    { items: [orderResponse({ items: [{ ...orderItem(), unitPriceMinor: Number.MAX_SAFE_INTEGER + 1 }] })] },
  ])("rejects malformed or unsafe history: %j", (body) => {
    expect(() => parseOrderHistory(body)).toThrow();
  });
});
