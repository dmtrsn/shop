"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/collection-navigation.module.css";

export default function CollectionError({
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
          <h1>Не удалось загрузить подборку</h1>
          <p>Сервис временно недоступен или вернул некорректный ответ. Попробуйте ещё раз.</p>
          <button onClick={reset} type="button">Повторить</button>{" "}
          <Link href="/collections">К списку подборок</Link>
          {error.digest ? <p>Код обращения: {error.digest}</p> : null}
        </div>
      </Container>
    </main>
  );
}
