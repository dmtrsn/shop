"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { QuantityControl } from "@/components/ui/quantity-control";

import styles from "./product-detail.module.css";

type ProductPurchaseProps = {
  minimumQuantity: number;
  productName: string;
  quantityStep: number;
  stock: number;
};

export function ProductPurchase({
  minimumQuantity,
  productName,
  quantityStep,
  stock,
}: ProductPurchaseProps) {
  const [quantity, setQuantity] = useState(minimumQuantity);
  const unavailable = stock < minimumQuantity;

  return (
    <div className={styles.purchaseControls}>
      <div>
        <p className={styles.quantityRules} id="quantity-rules">
          Минимум {minimumQuantity} · шаг {quantityStep}
        </p>
        <QuantityControl
          describedBy="quantity-rules"
          disabled={unavailable}
          label={`Количество: ${productName}`}
          min={minimumQuantity}
          onChange={setQuantity}
          step={quantityStep}
          value={quantity}
        />
      </div>
      <Button disabled>
        {unavailable ? "Покупка недоступна" : "Добавление в корзину пока недоступно"}
      </Button>
      <Button aria-label={`Избранное недоступно: ${productName}`} disabled variant="secondary">
        Избранное пока недоступно
      </Button>
      <p className={styles.readOnlyNote}>
        Количество меняется только на этой странице и не резервирует товар.
      </p>
    </div>
  );
}
