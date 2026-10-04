import { browserRequest, browserResponse, type PrivateRequestOptions } from "./browser-request";
import type { Cart } from "./cart";
import { parseOrder, parseOrderHistory, type Order } from "./order";

export type ExpectedCheckoutItem = { productId: string; quantity: number; unitPriceMinor: number };

// The exact POST /api/orders body. The expectation is copied from one
// authoritative Cart snapshot; Go compares it and never prices from it.
export type CheckoutBody = {
  deliveryAddress: string;
  comment?: string;
  expectedItems: ExpectedCheckoutItem[];
  expectedTotalMinor: number;
};

export type CheckoutReview = Pick<CheckoutBody, "expectedItems" | "expectedTotalMinor">;

// Selected lines and Go's own summary total; nothing is computed here.
export function checkoutReview(cart: Cart): CheckoutReview {
  return {
    expectedItems: cart.items.filter((line) => line.selected).map((line) => ({
      productId: line.product.id, quantity: line.quantity, unitPriceMinor: line.product.priceMinor,
    })),
    expectedTotalMinor: cart.summary.totalMinor,
  };
}

// Order creation can wait on PostgreSQL row locks; a short timeout would only
// produce more ambiguous outcomes. It is never retried here.
export const placeOrderTimeoutMs = 20_000;

export async function placeBrowserOrder(
  body: CheckoutBody, idempotencyKey: string, csrfToken: string, options: PrivateRequestOptions,
): Promise<{ order: Order; replayed: boolean }> {
  const response = await browserResponse("/api/orders", {
    timeoutMs: placeOrderTimeoutMs, ...options, method: "POST", body, csrfToken, idempotencyKey, success: 201,
  });
  return { order: parseOrder(response.body), replayed: response.headers.get("Idempotent-Replayed") === "true" };
}

export async function getBrowserOrder(orderID: string, options: PrivateRequestOptions): Promise<Order> {
  return parseOrder(await browserRequest(`/api/orders/${encodeURIComponent(orderID)}`, options));
}

export async function getBrowserOrders(options: PrivateRequestOptions): Promise<Order[]> {
  return parseOrderHistory(await browserRequest("/api/orders", options));
}

// A dynamic route segment arrives still percent-encoded (for example
// "%2310485" for the order number "#10485"), and getBrowserOrder encodes it
// again for Go, so it is decoded once. Order IDs and numbers never contain "%".
export function routeOrderID(segment: string): string {
  try { return decodeURIComponent(segment); } catch { return segment; }
}
