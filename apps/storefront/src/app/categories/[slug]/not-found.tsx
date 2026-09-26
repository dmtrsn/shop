import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/category-navigation.module.css";

export default function CategoryNotFound() {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <div className={styles.state}>
          <h1>Категория не найдена</h1>
          <p>Возможно, ссылка устарела или категория больше не опубликована.</p>
          <Link href="/categories">К списку категорий</Link>
        </div>
      </Container>
    </main>
  );
}
