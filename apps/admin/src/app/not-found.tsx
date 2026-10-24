import Link from "next/link";

import styles from "@/components/status-page.module.css";

export default function NotFound() {
  return (
    <main className={styles.page} id="main-content">
      <h1>Страница не найдена</h1>
      <p>Такой страницы в панели управления нет.</p>
      <Link className={styles.action} href="/">
        К обзору
      </Link>
    </main>
  );
}
