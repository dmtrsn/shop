"use client";

import { useId, type ChangeEvent } from "react";

import styles from "./ui.module.css";

export type QuantityControlProps = {
  describedBy?: string;
  disabled?: boolean;
  label?: string;
  min?: number;
  onChange: (value: number) => void;
  step?: number;
  value: number;
};

export function QuantityControl({
  describedBy,
  disabled = false,
  label = "Количество",
  min = 0,
  onChange,
  step = 1,
  value,
}: QuantityControlProps) {
  const id = useId();
  const safeStep = Number.isFinite(step) && step > 0 ? step : 1;
  const decrementDisabled = disabled || value <= min;

  function handleInput(event: ChangeEvent<HTMLInputElement>) {
    const nextValue = event.currentTarget.valueAsNumber;
    if (Number.isFinite(nextValue) && nextValue >= min) {
      onChange(nextValue);
    }
  }

  return (
    <div className={styles.quantity} role="group" aria-labelledby={`${id}-label`}>
      <span className="visually-hidden" id={`${id}-label`}>
        {label}
      </span>
      <button
        aria-label={`Уменьшить: ${label.toLowerCase()}`}
        className={styles.quantityButton}
        disabled={decrementDisabled}
        onClick={() => onChange(Math.max(min, value - safeStep))}
        type="button"
      >
        <span aria-hidden="true">−</span>
      </button>
      <input
        aria-describedby={describedBy}
        aria-label={label}
        className={styles.quantityInput}
        disabled={disabled}
        inputMode="numeric"
        min={min}
        onChange={handleInput}
        step={safeStep}
        type="number"
        value={value}
      />
      <button
        aria-label={`Увеличить: ${label.toLowerCase()}`}
        className={styles.quantityButton}
        disabled={disabled}
        onClick={() => onChange(value + safeStep)}
        type="button"
      >
        <span aria-hidden="true">+</span>
      </button>
    </div>
  );
}
