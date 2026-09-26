"use client";

import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/catalog.module.css";

type CatalogErrorProps = {
  error: Error & { digest?: string };
  reset: () => void;
};

export default function CatalogError({ error, reset }: CatalogErrorProps) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.errorState} role="alert">
          <p className={styles.eyebrow}>Каталог временно недоступен</p>
          <h1>Не удалось загрузить товары</h1>
          <p>Сервис не ответил или вернул данные в неожиданном формате. Попробуйте ещё раз.</p>
          <div>
            <button onClick={reset} type="button">Повторить</button>
            <Link href="/">На главную</Link>
          </div>
          {error.digest ? <small>Код ошибки: {error.digest}</small> : null}
        </div>
      </Container>
    </main>
  );
}
