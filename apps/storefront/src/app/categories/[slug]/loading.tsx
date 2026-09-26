import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/category-navigation.module.css";

export default function CategoryLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем категорию" className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <div className={styles.identity}>
          <p className={styles.eyebrow}>Категория</p>
          <h1>Загружаем данные</h1>
        </div>
      </Container>
    </main>
  );
}
