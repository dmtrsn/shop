import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/collection-navigation.module.css";

export default function CollectionLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем подборку" className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Подборка</p>
          <h1>Загружаем подборку</h1>
        </div>
        <div aria-hidden="true" className={styles.state}>
          <p>Подготавливаем описание и товары…</p>
        </div>
      </Container>
    </main>
  );
}
