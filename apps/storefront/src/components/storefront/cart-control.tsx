"use client";

import { LoginLink } from "./login-link";

import Link from "next/link";
import { useEffect, useId, useState } from "react";

import { Button } from "@/components/ui/button";
import { PlusIcon } from "@/components/ui/icons";
import type { Product } from "@/lib/api/product-dto";

import { useCart } from "./cart-provider";
import { CartQuantityEditor, hasCustomRules, QuantityStepper, quantityError, quantityRulesText } from "./cart-quantity-editor";
import styles from "./cart.module.css";

// Shared Cart control for every product surface (cards in all densities and
// product detail). It lazily initializes the Cart and never fetches products.
// Compact catalog rows lay the control out inline instead of full width.
export function CartControl({ product, detail = false, compact = false }: {
  product: Product;
  detail?: boolean;
  compact?: boolean;
}) {
  const cart = useCart();
  const { ensure } = cart;
  const id = useId();
  const [draft, setDraft] = useState(String(product.minimumQuantity));
  const [localError, setLocalError] = useState("");
  useEffect(() => { void ensure(); }, [ensure]);

  const busy = cart.mutationPending || cart.reconciling;
  const className = compact ? `${styles.control} ${styles.inline}` : styles.control;
  const line = cart.cart?.items.find((item) => item.product.id === product.id);
  if (line) {
    return (
      <div className={className}>
        <CartQuantityEditor
          busy={busy}
          commit={(quantity) => cart.mutate({ kind: "quantity", productID: product.id, quantity })}
          error={cart.errors[product.id]}
          product={line.product}
          quantity={line.quantity}
          showRules={detail || hasCustomRules(line.product)}
        />
        {line.issue ? <p className={styles.issue}>{line.issue}</p> : null}
        <Link className={styles.inCart} href="/cart" prefetch={false}>
          {detail ? "Перейти в корзину" : "В корзине"}
        </Link>
      </div>
    );
  }
  // An unavailable offer gets no Add action; its stock label explains why.
  // Signing in would not make it purchasable either.
  if (product.stock < product.minimumQuantity) {
    return detail ? <p className={styles.unavailable}>Покупка недоступна</p> : null;
  }
  if (cart.status === "anonymous") {
    return (
      <div className={className}>
        <LoginLink aria-label={`Войдите, чтобы купить: ${product.name}`} className={styles.signIn}>
          Войти, чтобы купить
        </LoginLink>
      </div>
    );
  }
  if (cart.status === "unavailable") {
    return (
      <div className={className}>
        <Button aria-label={`Корзина недоступна. Повторить проверку: ${product.name}`} onClick={() => void cart.refresh()} variant="secondary">
          Повторить проверку корзины
        </Button>
      </div>
    );
  }

  const loading = cart.status !== "ready" || !cart.cart;
  const pending = cart.pendingProductID === product.id;
  const error = localError || cart.errors[product.id] || "";
  const errorID = `${id}-error`;
  const rulesID = `${id}-rules`;
  const showRules = detail || hasCustomRules(product);

  async function add() {
    if (loading || busy) return;
    // Cards add the minimum; detail adds the explicitly chosen quantity. Both
    // are absolute quantities for a product that is not yet in the Cart.
    const quantity = detail ? draft : String(product.minimumQuantity);
    const validation = quantityError(quantity, product);
    setLocalError(validation);
    if (!validation) await cart.mutate({ kind: "add", productID: product.id, quantity: Number(quantity) });
  }

  // Local draft only; nothing is sent until Add.
  function step(direction: 1 | -1) {
    setLocalError("");
    if (quantityError(draft, product)) { setDraft(String(product.minimumQuantity)); return; }
    const next = Number(draft) + direction * product.quantityStep;
    if (next >= product.minimumQuantity && next <= product.stock) setDraft(String(next));
  }
  const draftNumber = Number(draft);
  const draftValid = !quantityError(draft, product);

  return (
    <div className={className}>
      {detail ? (
        <QuantityStepper
          decrementDisabled={draftValid && draftNumber <= product.minimumQuantity}
          describedBy={[rulesID, error ? errorID : ""].filter(Boolean).join(" ")}
          incrementDisabled={draftValid && draftNumber + product.quantityStep > product.stock}
          invalid={Boolean(error)}
          onChange={(value) => { setDraft(value); setLocalError(""); }}
          onDecrement={() => step(-1)}
          onIncrement={() => step(1)}
          onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); void add(); } }}
          productName={product.name}
          value={draft}
        />
      ) : null}
      {showRules ? <p className={styles.rules} id={rulesID}>{quantityRulesText(product)}</p> : null}
      <Button
        aria-busy={loading || pending}
        aria-describedby={error ? errorID : undefined}
        aria-disabled={loading || busy}
        aria-label={`Добавить в корзину: ${product.name}`}
        className={styles.add}
        onClick={() => void add()}
      >
        {pending ? "Добавляем…" : <><PlusIcon />В корзину</>}
      </Button>
      <p aria-live="polite" className={styles.error} id={errorID}>{error}</p>
    </div>
  );
}
