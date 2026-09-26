"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/producer-navigation.module.css";

export default function ManufacturersError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.pageEmpty} role="alert">
          <h1>Не удалось загрузить производителей</h1>
          <p>Сервис временно недоступен или вернул данные в неожиданном формате.</p>
          <button onClick={reset} type="button">Повторить</button>{" "}
          <Link href="/catalog">Открыть каталог</Link>
          {error.digest ? <p>Код обращения: {error.digest}</p> : null}
        </div>
      </Container>
    </main>
  );
}
