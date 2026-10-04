import type { RepeatLine, RepeatPreview } from "@/lib/api/repeat";
import { favoriteProduct } from "./favorites-fixtures";
import { cartItem, cartResponse } from "./cart-fixtures";
export function repeatLine(sourcePosition = 1, productId = "p01", overrides: Partial<RepeatLine> = {}): RepeatLine {
  return { sourcePosition, productId, name: `Исторический ${productId}`, sku: `OLD-${productId}`, image: "", requestedQuantity: 2,
    originalUnitPriceMinor: 45000, product: favoriteProduct(productId), existingCartQuantity: 1, existingCartSelected: false,
    suggestedQuantity: 2, proposedQuantity: 2, status: "available", priceChanged: true, priceDeltaMinor: 3900, ...overrides };
}
export function repeatPreview(...items: RepeatLine[]): RepeatPreview {
  return { orderId: "ord-1", orderNumber: "#10482", items: items.length ? items : [repeatLine()] };
}
export function repeatResult() {
  return { receipt: { sourceOrderId: "ord-1", sourceOrderNumber: "#10482", addedCount: 1, addedQuantity: 2,
    additions: [{ sourcePosition: 1, productId: "p01", quantity: 2 }] }, cart: cartResponse(cartItem("p01", 3)) };
}
