import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/product-detail.module.css";

// Next keeps the 404 status and noindex; this only supplies the Russian copy.
export default function NotFound() {
  return (
    <main className={styles.statePage} id="main-content">
      <Container className={styles.stateContent}>
        <h1>Страница не найдена</h1>
        <p>Возможно, ссылка устарела или страница была перемещена.</p>
        <div className={styles.stateActions}>
          <Link className={styles.stateLink} href="/">
            На главную
          </Link>
          <Link className={styles.stateLink} href="/catalog">
            Перейти в каталог
          </Link>
        </div>
      </Container>
    </main>
  );
}
