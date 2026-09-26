import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/producer-navigation.module.css";

export default function ManufacturerLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем производителя" className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <div className={styles.identity}>
          <p className={styles.eyebrow}>Производитель</p>
          <h1>Загружаем данные</h1>
        </div>
      </Container>
    </main>
  );
}
