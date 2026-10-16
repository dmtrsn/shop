"use client";

import Link from "next/link";

import type { CartItem } from "@/lib/api/cart";
import { TrashIcon } from "@/components/ui/icons";
import { formatMoney } from "@/lib/money";

import { CatalogProductImage } from "./catalog-product-image";
import { useCart } from "./cart-provider";
import { CartQuantityEditor, hasCustomRules } from "./cart-quantity-editor";
import styles from "./cart.module.css";

export function CartRow({ item }: { item: CartItem }) {
  const cart = useCart();
  const { product } = item;
  const busy = cart.mutationPending || cart.reconciling;
  const href = `/products/${product.slug}`;
  const oldPrice = product.oldPriceMinor !== undefined && product.oldPriceMinor > product.priceMinor
    ? product.oldPriceMinor : null;
  return (
    <article className={styles.row} data-cart-row={product.id}>
      {/* The whole 44px cell is the checkbox's hit area. */}
      <label className={styles.select}>
        <input
          aria-disabled={busy}
          aria-label={`Выбрать для заказа: ${product.name}`}
          checked={item.selected}
          onChange={() => {
            // Non-optimistic: the checkbox follows the confirmed Cart only.
            if (!busy) void cart.mutate({ kind: "selected", productID: product.id, selected: !item.selected });
          }}
          type="checkbox"
        />
      </label>
      {/* The name link is the accessible product link; the image is a pointer shortcut. */}
      <Link aria-hidden="true" className={styles.image} href={href} tabIndex={-1}>
        <CatalogProductImage image={product.image} name={product.name} />
      </Link>
      <div className={styles.identity}>
        <h3><Link href={href}>{product.name}</Link></h3>
        <p className={styles.meta}>Арт. {product.sku} · {product.unit}</p>
        <p className={item.issue ? styles.issue : styles.stock}>
          {item.issue ? `Проблема наличия: ${item.issue}` : `В наличии: ${item.available}`}
        </p>
      </div>
      <div className={styles.prices}>
        <p>
          <span className="visually-hidden">Цена за единицу: </span>
          {formatMoney(product.priceMinor, product.currency)}
          {oldPrice !== null ? <> <del>{formatMoney(oldPrice, product.currency)}</del></> : null}
        </p>
        <p className={styles.lineTotal}>
          <span>Сумма: </span>{formatMoney(item.lineTotalMinor, product.currency)}
        </p>
        {item.issue ? <p className={styles.excluded}>Не входит в итог</p> : null}
      </div>
      <div className={styles.rowControls}>
        <CartQuantityEditor
          busy={busy}
          commit={(quantity) => cart.mutate({ kind: "quantity", productID: product.id, quantity })}
          error={cart.errors[product.id]}
          product={product}
          quantity={item.quantity}
          showRules={hasCustomRules(product)}
        />
        <button
          aria-busy={cart.pendingProductID === product.id}
          aria-disabled={busy}
          aria-label={`Удалить из корзины: ${product.name}`}
          className={styles.remove}
          data-cart-remove={product.id}
          onClick={() => { if (!busy) void cart.mutate({ kind: "remove", productID: product.id }); }}
          type="button"
        >
          <TrashIcon aria-hidden="true" />
          Удалить
        </button>
      </div>
    </article>
  );
}
