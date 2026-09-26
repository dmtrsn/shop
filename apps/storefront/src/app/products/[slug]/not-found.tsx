import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/product-detail.module.css";

export default function ProductNotFound() {
  return (
    <main className={styles.statePage} id="main-content">
      <Container className={styles.stateContent}>
        <h1>Товар не найден</h1>
        <p>Возможно, ссылка устарела или товар больше недоступен.</p>
        <div className={styles.stateActions}>
          <Link className={styles.stateLink} href="/">
            На главную
          </Link>
        </div>
      </Container>
    </main>
  );
}
