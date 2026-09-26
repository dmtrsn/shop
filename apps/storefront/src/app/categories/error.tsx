"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/category-navigation.module.css";

export default function CategoriesError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.state} role="alert">
          <h1>Не удалось загрузить категории</h1>
          <p>Сервис временно недоступен или вернул некорректный ответ. Попробуйте ещё раз.</p>
          <button onClick={reset} type="button">Повторить</button>{" "}
          <Link href="/catalog">Открыть каталог</Link>
          {error.digest ? <p>Код обращения: {error.digest}</p> : null}
        </div>
      </Container>
    </main>
  );
}
