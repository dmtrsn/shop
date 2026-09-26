"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/collection-navigation.module.css";

export default function HomeError({
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
          <h1>Не удалось открыть главную страницу</h1>
          <p>Произошла непредвиденная ошибка. Попробуйте ещё раз или перейдите в каталог.</p>
          <button onClick={reset} type="button">Повторить</button>{" "}
          <Link href="/catalog">Открыть каталог</Link>
          {error.digest ? <p>Код обращения: {error.digest}</p> : null}
        </div>
      </Container>
    </main>
  );
}
