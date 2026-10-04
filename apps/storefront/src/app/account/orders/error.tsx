"use client";

import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import styles from "@/components/storefront/cart.module.css";

export default function OrdersError({ retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return <main className={styles.main} id="main-content"><Container>
    <section className={styles.state} role="alert">
      <h1>Не удалось открыть историю заказов</h1>
      <p>Попробуйте загрузить страницу ещё раз.</p>
      <div className={styles.actions}>
        <Button onClick={retry} variant="secondary">Повторить</Button>
        <Link href="/catalog">Перейти в каталог</Link>
      </div>
    </section>
  </Container></main>;
}
