"use client";

import type { Product } from "@/lib/api/product-dto";

import { CartControl } from "./cart-control";
import { FavoriteButton } from "./favorite-button";
import styles from "./product-detail.module.css";

export function ProductPurchase({ product }: { product: Product }) {
  return (
    // The real Cart control leads; the favorite is a separate compact square.
    <div className={styles.purchaseControls}>
      <div className={styles.cart}><CartControl detail product={product} /></div>
      <div className={styles.favorite}><FavoriteButton productID={product.id} productName={product.name} /></div>
      <p className={styles.readOnlyNote}>Корзина не резервирует товар.</p>
    </div>
  );
}
