import { afterEach, describe, expect, it, vi } from "vitest";

import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { jsonResponse } from "@/test/favorites-fixtures";
import { orderResponse } from "@/test/order-fixtures";

import { ResponseOwnerError } from "./browser-request";
import { checkoutReview, getBrowserOrder, getBrowserOrders, placeBrowserOrder } from "./order-client";

afterEach(() => { vi.restoreAllMocks(); });

const body = { deliveryAddress: " Адрес ", comment: " c ", expectedItems: [{ productId: "p01", quantity: 2, unitPriceMinor: 48900 }], expectedTotalMinor: 126800 };

describe("order client", () => {
  it("copies the expectation from the selected Cart lines and Go's total only", () => {
    const cart = cartResponse(cartItem("p01", 2), cartItem("p02", 1, false), cartItem("p03", 3));
    cart.summary.totalMinor = 777;
    expect(checkoutReview(cart)).toEqual({
      expectedItems: [{ productId: "p01", quantity: 2, unitPriceMinor: 48900 }, { productId: "p03", quantity: 3, unitPriceMinor: 48900 }],
      expectedTotalMinor: 777,
    });
  });

  it("POSTs the exact body with idempotency, CSRF and expected-user headers and a 20s timeout", async () => {
    const timeout = vi.spyOn(AbortSignal, "timeout");
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(orderResponse(), 201, "A"));
    const result = await placeBrowserOrder(body, "key-1", "csrf-A", { expectedUserID: "A", fetcher });
    expect(result).toMatchObject({ replayed: false, order: { id: "ord-2026-10484" } });
    expect(timeout).toHaveBeenCalledWith(20_000);
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/orders", expect.objectContaining({ method: "POST", credentials: "same-origin", cache: "no-store" }));
    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(init.body as string)).toEqual(body);
    expect(init.headers).toEqual({
      Accept: "application/json", "Idempotency-Key": "key-1", "X-CSRF-Token": "csrf-A",
      "Content-Type": "application/json", "X-Expected-Authenticated-User-ID": "A",
    });
  });

  it("reports an idempotent replay from the response header", async () => {
    const response = jsonResponse(orderResponse(), 201, "A");
    response.headers.set("Idempotent-Replayed", "true");
    const result = await placeBrowserOrder(body, "key-1", "csrf", { expectedUserID: "A", fetcher: vi.fn().mockResolvedValue(response) });
    expect(result.replayed).toBe(true);
  });

  it.each([
    [401, "unauthorized", null], [403, "csrf_failed", null], [409, "checkout_changed", "A"], [409, "out_of_stock", "A"],
    [409, "conflict", "A"], [409, "idempotency_key_reused", "A"], [422, "validation_error", "A"],
  ])("exposes %s %s with its code", async (status, code, owner) => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ error: { code, message: "m" } }, status, owner));
    await expect(placeBrowserOrder(body, "k", "c", { expectedUserID: "A", fetcher })).rejects.toMatchObject({ status, code });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("never retries: network failure, 5xx, a 200 instead of 201 and a malformed 201 are each one request", async () => {
    for (const make of [
      () => Promise.reject(new TypeError("offline")),
      () => Promise.resolve(jsonResponse({ error: { code: "internal_error" } }, 500, "A")),
      () => Promise.resolve(jsonResponse(orderResponse(), 200, "A")),
      () => Promise.resolve(jsonResponse({ id: "ord" }, 201, "A")),
      () => Promise.resolve(new Response("<html>", { status: 201, headers: { "X-Authenticated-User-ID": "A" } })),
    ]) {
      const fetcher = vi.fn().mockImplementation(make);
      await expect(placeBrowserOrder(body, "k", "c", { expectedUserID: "A", fetcher })).rejects.toBeInstanceOf(Error);
      expect(fetcher).toHaveBeenCalledTimes(1);
    }
  });

  it("GETs one owned order by encoded ID and rejects a foreign owner unread", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(orderResponse(), 200, "A"));
    await getBrowserOrder("#10484", { expectedUserID: "A", fetcher });
    expect(fetcher).toHaveBeenCalledWith("/api/orders/%2310484", expect.objectContaining({ method: "GET" }));
    const foreign = jsonResponse(orderResponse(), 200, "B");
    const json = vi.spyOn(foreign, "json");
    await expect(getBrowserOrder("x", { expectedUserID: "A", fetcher: vi.fn().mockResolvedValue(foreign) })).rejects.toBeInstanceOf(ResponseOwnerError);
    expect(json).not.toHaveBeenCalled();
    await expect(getBrowserOrder("x", { expectedUserID: "A", fetcher: vi.fn().mockResolvedValue(jsonResponse({ error: { code: "not_found" } }, 404, "A")) }))
      .rejects.toEqual(expect.objectContaining({ status: 404 }));
  });
});

describe("history client", () => {
  it("gets the complete history once, no-store and bound to the expected owner", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ items: [orderResponse({ items: null })] }));
    expect(await getBrowserOrders({ expectedUserID: "A", fetcher })).toMatchObject([{ items: [], itemCount: 2 }]);
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/orders", expect.objectContaining({
      method: "GET", cache: "no-store", credentials: "same-origin",
      headers: { Accept: "application/json", "X-Expected-Authenticated-User-ID": "A" },
    }));
  });

  it.each([null, "B"])("rejects owner %j before reading private data", async (owner) => {
    const response = jsonResponse({ items: [orderResponse()] }, 200, owner);
    const read = vi.spyOn(response, "json");
    await expect(getBrowserOrders({ expectedUserID: "A", fetcher: vi.fn().mockResolvedValue(response) })).rejects.toBeInstanceOf(ResponseOwnerError);
    expect(read).not.toHaveBeenCalled();
  });

  it.each([[401, "unauthorized"], [500, "internal_error"]])("reports %s without retry", async (status, code) => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ error: { code } }, status, null));
    await expect(getBrowserOrders({ expectedUserID: "A", fetcher })).rejects.toMatchObject({ status, code });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("does not turn transport failure into empty history", async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError("offline"));
    await expect(getBrowserOrders({ expectedUserID: "A", fetcher })).rejects.toThrow();
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});
