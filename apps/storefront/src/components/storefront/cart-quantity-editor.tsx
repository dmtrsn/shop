"use client";

import { useId, useRef, useState, type KeyboardEvent } from "react";

import type { Product } from "@/lib/api/product-dto";

import styles from "./cart.module.css";

type QuantityRules = Pick<Product, "minimumQuantity" | "quantityStep" | "stock">;

// Local convenience validation only; Go remains authoritative. Zero and values
// below the minimum are never a deletion request.
export function quantityError(draft: string, rules: QuantityRules): string {
  const quantity = Number(draft);
  if (!/^\d+$/.test(draft) || !Number.isSafeInteger(quantity)) return "Введите целое количество.";
  if (quantity < rules.minimumQuantity) {
    return `Минимум ${rules.minimumQuantity}. Чтобы убрать товар, используйте «Удалить».`;
  }
  if ((quantity - rules.minimumQuantity) % rules.quantityStep !== 0) {
    return `Количество должно быть ${rules.minimumQuantity} и далее с шагом ${rules.quantityStep}.`;
  }
  if (quantity > rules.stock) return `Доступно ${rules.stock}. Уменьшите количество.`;
  return "";
}

// Default rules (minimum 1, step 1) are not worth repeating on every line.
export function hasCustomRules(rules: Pick<Product, "minimumQuantity" | "quantityStep">): boolean {
  return rules.minimumQuantity !== 1 || rules.quantityStep !== 1;
}

export function quantityRulesText(rules: QuantityRules): string {
  return `Минимум ${rules.minimumQuantity} · шаг ${rules.quantityStep}`;
}

function describedBy(...ids: Array<string | false>) {
  return ids.filter(Boolean).join(" ") || undefined;
}

type StepperProps = {
  productName: string;
  value: string;
  invalid: boolean;
  describedBy?: string;
  decrementDisabled: boolean;
  incrementDisabled: boolean;
  onChange: (value: string) => void;
  onKeyDown?: (event: KeyboardEvent<HTMLInputElement>) => void;
  onDecrement: () => void;
  onIncrement: () => void;
};

// Text input keeps the exact string draft, including invalid values that need
// correction. Disabled states use aria-disabled so focus never drops.
export function QuantityStepper(props: StepperProps) {
  const { productName } = props;
  return (
    <div aria-label={`Количество: ${productName}`} className={styles.stepper} role="group">
      <button
        aria-disabled={props.decrementDisabled}
        aria-label={`Уменьшить количество: ${productName}`}
        onClick={() => { if (!props.decrementDisabled) props.onDecrement(); }}
        type="button"
      >
        <span aria-hidden="true">−</span>
      </button>
      <input
        aria-describedby={props.describedBy}
        aria-invalid={props.invalid}
        aria-label={`Количество: ${productName}`}
        autoComplete="off"
        inputMode="numeric"
        onChange={(event) => props.onChange(event.target.value)}
        onKeyDown={props.onKeyDown}
        type="text"
        value={props.value}
      />
      <button
        aria-disabled={props.incrementDisabled}
        aria-label={`Увеличить количество: ${productName}`}
        onClick={() => { if (!props.incrementDisabled) props.onIncrement(); }}
        type="button"
      >
        <span aria-hidden="true">+</span>
      </button>
    </div>
  );
}

type EditorProps = {
  product: Product;
  quantity: number;
  busy: boolean;
  error?: string;
  showRules?: boolean;
  commit: (quantity: number) => Promise<boolean>;
};

// Editor for a confirmed cart line. Typing only edits a local draft; Enter or
// Apply commits it, Escape restores the confirmed value, and +/- commit one
// deliberate absolute quantity. Nothing is queued while a write is pending.
export function CartQuantityEditor({ product, quantity, busy, error, showRules = true, commit }: EditorProps) {
  const id = useId();
  const edits = useRef(0);
  const [draft, setDraft] = useState<string | null>(null);
  const [localError, setLocalError] = useState("");
  const [dismissedError, setDismissedError] = useState<string | undefined>();
  const value = draft ?? String(quantity);
  const feedback = localError || (error && error !== dismissedError ? error : "");
  const dirty = draft !== null && draft !== String(quantity);
  const atMinimum = quantity <= product.minimumQuantity;
  const atStock = quantity + product.quantityStep > product.stock;
  const rulesID = `${id}-rules`;
  const errorID = `${id}-error`;

  async function apply(next: string) {
    if (busy) return;
    setDraft(next);
    setDismissedError(undefined);
    const validation = quantityError(next, product);
    setLocalError(validation);
    if (validation) return;
    if (Number(next) === quantity) { setDraft(null); return; }
    const version = ++edits.current;
    // The confirmed value changes only through the server response. Newer
    // typing during the request is kept.
    if (await commit(Number(next)) && edits.current === version) setDraft(null);
  }

  function edit(next: string) {
    edits.current++;
    setDraft(next);
    setLocalError("");
    setDismissedError(error);
  }

  function reset() {
    edits.current++;
    setDraft(null);
    setLocalError("");
    setDismissedError(error);
  }

  return (
    <div className={styles.editor}>
      <div className={styles.editorRow}>
        <QuantityStepper
          decrementDisabled={busy || atMinimum}
          describedBy={describedBy(showRules && rulesID, Boolean(feedback) && errorID)}
          incrementDisabled={busy || atStock}
          invalid={Boolean(feedback)}
          onChange={edit}
          onDecrement={() => void apply(String(quantity - product.quantityStep))}
          onIncrement={() => void apply(String(quantity + product.quantityStep))}
          onKeyDown={(event) => {
            if (event.key === "Enter") { event.preventDefault(); void apply(value); }
            if (event.key === "Escape" && draft !== null) { event.preventDefault(); reset(); }
          }}
          productName={product.name}
          value={value}
        />
        {dirty ? (
          <button
            aria-disabled={busy}
            aria-label={`Применить количество: ${product.name}`}
            className={styles.apply}
            onClick={() => void apply(value)}
            type="button"
          >
            Применить
          </button>
        ) : null}
      </div>
      {showRules ? <p className={styles.rules} id={rulesID}>{quantityRulesText(product)}</p> : null}
      <p aria-live="polite" className={styles.error} id={errorID}>{feedback}</p>
    </div>
  );
}
