"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/product-detail.module.css";

type ProductErrorProps = {
  error: Error & { digest?: string };
  reset: () => void;
};

export default function ProductError({ error, reset }: ProductErrorProps) {
  return (
    <main className={styles.statePage} id="main-content">
      <Container className={styles.stateContent}>
        <h1>Не удалось загрузить товар</h1>
        <p>Сервис временно недоступен. Попробуйте запрос ещё раз.</p>
        {error.digest ? <p className={styles.reference}>Код обращения: {error.digest}</p> : null}
        <div className={styles.stateActions}>
          <button className={styles.retryButton} onClick={reset} type="button">
            Повторить
          </button>
          <Link className={styles.stateLink} href="/">
            На главную
          </Link>
        </div>
      </Container>
    </main>
  );
}
