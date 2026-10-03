"use client";

import type { Product } from "@/lib/api/product-dto";

import { CartControl } from "./cart-control";
import { FavoriteButton } from "./favorite-button";
import styles from "./product-detail.module.css";

export function ProductPurchase({ product }: { product: Product }) {
  return (
    <div className={styles.purchaseControls}>
      <CartControl detail product={product} />
      <FavoriteButton productID={product.id} productName={product.name} text />
      <p className={styles.readOnlyNote}>Корзина не резервирует товар.</p>
    </div>
  );
}
