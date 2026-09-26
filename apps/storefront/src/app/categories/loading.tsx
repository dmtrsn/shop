import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/category-navigation.module.css";

export default function CategoriesLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем категории" className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Каталог</p>
          <h1>Загружаем категории</h1>
        </div>
        <div aria-hidden="true" className={styles.state}>
          <p>Подготавливаем справочник категорий…</p>
        </div>
      </Container>
    </main>
  );
}
