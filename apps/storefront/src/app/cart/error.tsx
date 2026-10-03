"use client";

import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import styles from "@/components/storefront/cart.module.css";

export default function CartError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <main className={styles.main} id="main-content"><Container>
    <div className={styles.state} role="alert">
      <h1>Не удалось открыть корзину</h1>
      <p>Попробуйте загрузить страницу ещё раз.</p>
      <div className={styles.actions}>
        <Button onClick={reset} variant="secondary">Повторить</Button>
        <Link href="/catalog">Перейти в каталог</Link>
      </div>
    </div>
  </Container></main>;
}
