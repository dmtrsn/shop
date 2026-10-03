import type { Cart, CartItem } from "@/lib/api/cart";

import { favoriteProduct } from "./favorites-fixtures";

export function cartItem(id = "p01", quantity = 1, selected = true): CartItem {
  const product = favoriteProduct(id);
  return { product, quantity, selected, available: product.stock, lineTotalMinor: quantity * product.priceMinor };
}

// Fixture shaped like Go's response. Tests assert that the storefront displays
// these values verbatim; it never recomputes them.
export function cartResponse(...items: CartItem[]): Cart {
  const groups: Cart["groups"] = [];
  for (const line of items) {
    let group = groups.find((candidate) => candidate.manufacturer.id === line.product.manufacturer.id);
    if (!group) {
      group = { manufacturer: line.product.manufacturer, items: [], selectedCount: 0, subtotalMinor: 0 };
      groups.push(group);
    }
    group.items.push(line);
    if (line.selected) {
      group.selectedCount += line.quantity;
      if (!line.issue) group.subtotalMinor += line.lineTotalMinor;
    }
  }
  const subtotalMinor = groups.reduce((sum, group) => sum + group.subtotalMinor, 0);
  const deliveryMinor = subtotalMinor > 0 && subtotalMinor < 350000 ? 29000 : 0;
  return {
    items,
    groups,
    updatedAt: "0001-01-01T00:00:00Z",
    summary: {
      positionCount: items.length,
      itemCount: items.reduce((sum, line) => sum + line.quantity, 0),
      selectedCount: groups.reduce((sum, group) => sum + group.selectedCount, 0),
      subtotalMinor,
      discountMinor: 0,
      deliveryMinor,
      totalMinor: subtotalMinor + deliveryMinor,
      unavailableCount: items.filter((line) => line.issue).length,
    },
  };
}
