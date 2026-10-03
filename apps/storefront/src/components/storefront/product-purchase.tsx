"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { QuantityControl } from "@/components/ui/quantity-control";

import styles from "./product-detail.module.css";
import { FavoriteButton } from "./favorite-button";

type ProductPurchaseProps = {
  minimumQuantity: number;
  productName: string;
  productID: string;
  quantityStep: number;
  stock: number;
};

export function ProductPurchase({
  minimumQuantity,
  productName,
  productID,
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
      <FavoriteButton productID={productID} productName={productName} text />
      <p className={styles.readOnlyNote}>
        Количество меняется только на этой странице и не резервирует товар.
      </p>
    </div>
  );
}
