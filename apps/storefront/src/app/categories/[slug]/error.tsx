"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/category-navigation.module.css";

export default function CategoryError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <div className={styles.state} role="alert">
          <h1>Не удалось загрузить категорию</h1>
          <p>Сервис временно недоступен. Попробуйте запрос ещё раз.</p>
          <button onClick={reset} type="button">Повторить</button>{" "}
          <Link href="/categories">К списку категорий</Link>
          {error.digest ? <p>Код обращения: {error.digest}</p> : null}
        </div>
      </Container>
    </main>
  );
}
