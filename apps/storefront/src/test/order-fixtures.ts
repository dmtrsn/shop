import type { OrderItem } from "@/lib/api/order";

export function orderItem(id = "p01", quantity = 2): OrderItem {
  return {
    productId: id, sku: `SKU-${id}`, name: `Шоколад ${id}`, image: "", manufacturerId: "maker", manufacturer: "Мастерская",
    quantity, unitPriceMinor: 48900, lineTotalMinor: quantity * 48900,
  };
}

// Shaped exactly like Go's Order JSON, including the zero deliveredAt.
export function orderResponse(overrides: Record<string, unknown> = {}) {
  return {
    id: "ord-2026-10484", number: "#10484", status: "processing", statusLabel: "Собираем заказ",
    items: [orderItem()], itemCount: 2, totalMinor: 126800, deliveryAddress: "Москва, Тестовая, 1",
    createdAt: "2026-10-03T12:00:00Z", deliveredAt: "0001-01-01T00:00:00Z", ...overrides,
  };
}
