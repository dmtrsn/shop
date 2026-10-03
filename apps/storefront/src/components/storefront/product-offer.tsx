"use client";

import type { Product } from "@/lib/api/product-dto";
import { formatMoney } from "@/lib/money";

import { useCart } from "./cart-provider";
import catalogStyles from "./catalog.module.css";
import detailStyles from "./product-detail.module.css";

function stockLabel(product: Product, detail: boolean): string {
  if (product.stock <= 0) return "Нет в наличии";
  if (product.stock < product.minimumQuantity) {
    return detail ? "Недостаточно для минимального заказа" : "Меньше минимального количества";
  }
  return "В наличии";
}

// Price and stock of a product surface. Reading an already initialized Cart
// issues no request; once Go confirms a line, its product offer supersedes the
// older server-rendered catalog/detail props so card and Cart never disagree.
export function ProductOffer({ product: rendered, part, detail = false }: {
  product: Product;
  part: "price" | "stock";
  detail?: boolean;
}) {
  const { cart } = useCart();
  const product = cart?.items.find((line) => line.product.id === rendered.id)?.product ?? rendered;
  if (part === "price") {
    const oldPrice = product.oldPriceMinor !== undefined && product.oldPriceMinor > product.priceMinor
      ? product.oldPriceMinor : null;
    if (detail) {
      return (
        <div aria-label="Цена" className={detailStyles.priceGroup}>
          <span className={detailStyles.price}>{formatMoney(product.priceMinor, product.currency)}</span>
          {oldPrice !== null ? <del className={detailStyles.oldPrice}>{formatMoney(oldPrice, product.currency)}</del> : null}
        </div>
      );
    }
    return (
      <p aria-label="Цена" className={catalogStyles.price}>
        <strong>{formatMoney(product.priceMinor, product.currency)}</strong>
        {oldPrice !== null ? <del>{formatMoney(oldPrice, product.currency)}</del> : null}
      </p>
    );
  }
  const available = product.stock >= product.minimumQuantity;
  const className = detail
    ? available ? detailStyles.stockAvailable : detailStyles.stockUnavailable
    : available ? catalogStyles.inStock : catalogStyles.outOfStock;
  return (
    <p className={className}>
      <span aria-hidden="true" className={detail ? detailStyles.stockMarker : undefined} />
      {stockLabel(product, detail)}
    </p>
  );
}
