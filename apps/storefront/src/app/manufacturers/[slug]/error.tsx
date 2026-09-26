"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/producer-navigation.module.css";

export default function ManufacturerError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <div className={styles.pageEmpty} role="alert">
          <h1>Не удалось загрузить производителя</h1>
          <p>Сервис временно недоступен. Попробуйте запрос ещё раз.</p>
          <button onClick={reset} type="button">Повторить</button>{" "}
          <Link href="/manufacturers">К списку производителей</Link>
          {error.digest ? <p>Код обращения: {error.digest}</p> : null}
        </div>
      </Container>
    </main>
  );
}
