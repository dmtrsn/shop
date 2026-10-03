import { browserRequest, type PrivateRequestOptions } from "./browser-request";
import { parseCart, type Cart } from "./cart";

// Every command returns the complete authoritative Cart. Quantities are
// absolute assignments: POST never means "existing + delta" and 0 never deletes.
export type CartCommand =
  | { kind: "add"; productID: string; quantity: number }
  | { kind: "quantity"; productID: string; quantity: number }
  | { kind: "selected"; productID: string; selected: boolean }
  | { kind: "remove"; productID: string }
  | { kind: "selection"; productIDs: string[]; selected: boolean };

type Request = { path: string; method: "POST" | "PATCH" | "DELETE"; body?: unknown; success: number };

// Bulk selection always names explicit canonical IDs. Omitted, null or empty
// productIds must never reach Go, so this boundary rejects them before fetch.
export function assertExplicitSelection(productIDs: readonly string[]) {
  if (!Array.isArray(productIDs) || !productIDs.length ||
      productIDs.some((id) => typeof id !== "string" || !id.trim()) ||
      new Set(productIDs).size !== productIDs.length) {
    throw new Error("Cart selection requires explicit nonempty canonical product IDs");
  }
}

function cartRequest(command: CartCommand): Request {
  const item = (productID: string) => `/api/cart/items/${encodeURIComponent(productID)}`;
  switch (command.kind) {
    case "add":
      return { path: "/api/cart/items", method: "POST", body: { productId: command.productID, quantity: command.quantity }, success: 201 };
    case "quantity":
      return { path: item(command.productID), method: "PATCH", body: { quantity: command.quantity }, success: 200 };
    case "selected":
      return { path: item(command.productID), method: "PATCH", body: { selected: command.selected }, success: 200 };
    case "remove":
      return { path: item(command.productID), method: "DELETE", success: 200 };
    case "selection":
      assertExplicitSelection(command.productIDs);
      return {
        path: "/api/cart/selection", method: "PATCH",
        body: { productIds: [...command.productIDs], selected: command.selected }, success: 200,
      };
  }
}

export async function getBrowserCart(options: PrivateRequestOptions): Promise<Cart> {
  return parseCart(await browserRequest("/api/cart", options));
}

export async function mutateBrowserCart(command: CartCommand, csrfToken: string, options: PrivateRequestOptions): Promise<Cart> {
  const request = cartRequest(command);
  return parseCart(await browserRequest(request.path, { ...options, ...request, csrfToken }));
}
