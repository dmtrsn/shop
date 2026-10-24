"use client";

import styles from "@/components/status-page.module.css";

// Never shows the error itself: it may carry internal details.
export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main className={styles.page} id="main-content">
      <h1>Что-то пошло не так</h1>
      <p>Страницу не удалось показать. Повторите попытку.</p>
      <button className={styles.action} onClick={reset} type="button">
        Повторить
      </button>
    </main>
  );
}
