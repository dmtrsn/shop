import styles from "./status-page.module.css";

// Go could not confirm the staff session. That is neither signed in nor
// signed out, so the page neither shows staff content nor redirects.
export function ServiceUnavailable() {
  return (
    <main className={styles.page} id="main-content">
      <h1>Панель управления недоступна</h1>
      <p>Не удалось проверить вход: сервис не отвечает. Повторите попытку через минуту.</p>
      <a className={styles.action} href="/">
        Повторить
      </a>
    </main>
  );
}
