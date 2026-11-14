"use client";

import Link from "next/link";
import { memo, useId, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/text-input";
import { browserRequest } from "@/lib/api/browser-request";
import type { CartItem } from "@/lib/api/cart";
import { parseProduct, type Product } from "@/lib/api/product-dto";
import { formatMoney } from "@/lib/money";
import { adjustQuantity, checkQuantity, type QuantityResult } from "@/lib/quick-order";

import { LoginLink } from "./login-link";
import type { QuickOrderCart } from "./quick-order-view";
import styles from "./quick-order.module.css";

function quantityProblem(result: QuantityResult): string {
  const { minimumQuantity, quantityStep, stock } = result.rules;
  switch (result.state) {
    case "empty": return "Укажите количество.";
    case "invalid-integer": return "Введите целое число.";
    case "below-minimum": return `Минимальное количество: ${minimumQuantity}.`;
    case "invalid-step": return `Допустимые количества: от ${minimumQuantity} с шагом ${quantityStep}.`;
    case "above-stock": return `Доступно только ${stock}. Уменьшите количество.`;
    case "valid": return "";
  }
}

// Only an explicit submission installs temporary focus listeners. Rows have no
// cart-loading effects; their confirmed lines come from the boundary's lookup.
type RowProps = {
  product: Product; cart: QuickOrderCart; line?: CartItem;
  prepareFocus: () => () => void; refreshProduct: (product: Product) => void;
};

const RowContent = memo(function RowContent({ product, cart, line, prepareFocus, refreshProduct }: RowProps) {
  const [draft, setDraft] = useState("");
  const [attempted, setAttempted] = useState(false);
  const [failure, setFailure] = useState("");
  const [pending, setPending] = useState(false);
  const editGeneration = useRef(0);
  const submitting = useRef(false);
  const form = useRef<HTMLFormElement>(null);
  const id = useId();
  const validation = checkQuantity(draft, product);
  const unavailable = !validation.purchasable;
  const problem = attempted || draft !== "" ? quantityProblem(validation) : "";
  const error = problem || failure;
  const busy = cart.mutationPending || cart.reconciling || pending;
  const blocked = busy || cart.status !== "ready" || !cart.cart || unavailable;
  const rulesID = `${id}-rules`;
  const errorID = `${id}-error`;
  const action = line ? "Обновить количество" : "Добавить";
  const minus = adjustQuantity(draft, -1, product);
  const plus = adjustQuantity(draft, 1, product);

  function edit(value: string) {
    ++editGeneration.current;
    setDraft(value); setAttempted(false); setFailure("");
  }

  async function submit(value = draft) {
    setAttempted(true);
    if (blocked || submitting.current || checkQuantity(value, product).state !== "valid") return;
    const node = form.current;
    if (!node) return;
    const version = editGeneration.current;
    const restore = prepareFocus();
    let ownsFocus = node.contains(document.activeElement);
    const moved = (event: Event) => { if (!node.contains(event.target as Node)) ownsFocus = false; };
    const blurred = () => { ownsFocus = false; };
    document.addEventListener("focusin", moved);
    document.addEventListener("pointerdown", moved);
    window.addEventListener("blur", blurred);
    submitting.current = true; setPending(true); setFailure("");
    try {
      const success = await cart.mutate({ kind: line ? "quantity" : "add", productID: product.id, quantity: Number(value) });
      if (!node.isConnected) return;
      if (success) {
        if (version === editGeneration.current) { setDraft(""); setAttempted(false); }
        if (ownsFocus && version === editGeneration.current && document.hasFocus()) restore();
      } else {
        setFailure("Не удалось подтвердить изменение. Проверьте корзину и повторите действие.");
        // CartProvider reconciles uncertain writes. Independently refresh public
        // rules from the product endpoint; never extract numbers from prose.
        try {
          const refreshed = parseProduct(await browserRequest(`/api/products/${encodeURIComponent(product.id)}`));
          if (node.isConnected) refreshProduct(refreshed);
        } catch {
          if (node.isConnected) setFailure("Не удалось обновить наличие и правила количества. Повторите действие позже.");
        }
      }
    } finally {
      document.removeEventListener("focusin", moved);
      document.removeEventListener("pointerdown", moved);
      window.removeEventListener("blur", blurred);
      submitting.current = false;
      if (node.isConnected) setPending(false);
    }
  }

  return <>
    <div className={styles.productInfo}>
      <Link className={styles.productName} href={`/products/${encodeURIComponent(product.slug)}`}>{product.name}</Link>
      {product.sku ? <span className={styles.secondary}>арт. {product.sku}</span> : null}
      <span className={unavailable ? styles.unavailable : styles.secondary}>
        {product.stock === 0 ? "Нет в наличии" : unavailable ? `Доступно ${product.stock} ${product.unit} — меньше минимального количества ${product.minimumQuantity} ${product.unit}` : `В наличии: ${product.stock} ${product.unit}`}
      </span>
      {line ? <span className={styles.inCart}>✓ В корзине: {line.quantity} {product.unit}</span> : null}
      {line?.issue ? <span className={styles.unavailable}>{line.issue}</span> : null}
    </div>
    <p className={styles.price}>{formatMoney(product.priceMinor, product.currency)} <span className={styles.secondary}>/ {product.unit}</span></p>
    <form aria-label={`Заказать: ${product.name}`} className={styles.quantity} onSubmit={(event) => { event.preventDefault(); void submit(); }} ref={form}>
      <div className={styles.quantityControls}>
        <Button aria-label={`Уменьшить количество: ${product.name}`} disabled={unavailable || minus === undefined || busy} onClick={() => { if (minus !== undefined) edit(String(minus)); }} size="sm" variant="secondary">−</Button>
        <TextInput aria-describedby={`${rulesID} ${errorID}`} aria-invalid={Boolean(problem)} autoComplete="off" disabled={unavailable}
          inputMode="numeric" label={`Количество: ${product.name}`} visuallyHideLabel onChange={(event) => edit(event.target.value)} type="text" value={draft} />
        <Button aria-label={`Увеличить количество: ${product.name}`} disabled={unavailable || plus === undefined || busy} onClick={() => { if (plus !== undefined) edit(String(plus)); }} size="sm" variant="secondary">+</Button>
        <Button aria-disabled={blocked} aria-busy={pending} aria-label={`${action}: ${product.name}`} disabled={unavailable} size="sm" type="submit">{pending ? "Сохраняем…" : action}</Button>
      </div>
      <p className={styles.secondary} id={rulesID}>В {product.unit}: от {product.minimumQuantity} с шагом {product.quantityStep}</p>
      <div aria-live="polite" className={styles.error} id={errorID}>
        {error ? <p>{error}</p> : null}
        {failure && cart.errors[product.id] ? <p>{cart.errors[product.id]}</p> : null}
        {problem && validation.suggestion !== undefined ? <Button aria-disabled={blocked} onClick={() => void submit(String(validation.suggestion))} size="sm" variant="secondary">
          {line ? "Обновить до" : "Добавить"} {validation.suggestion}
        </Button> : null}
      </div>
      {cart.status === "anonymous" && !unavailable ? <LoginLink>Войти, чтобы купить</LoginLink> : null}
    </form>
  </>;
});

// Keep ordinary rows mounted during local search. Only their visibility changes,
// so broadening a query does not recreate hundreds of labelled form controls.
export function QuickOrderRow({ hidden, ...props }: RowProps & { hidden: boolean }) {
  return <li className={`${styles.row} ${props.line ? styles.confirmed : ""}`} hidden={hidden}><RowContent {...props} /></li>;
}
