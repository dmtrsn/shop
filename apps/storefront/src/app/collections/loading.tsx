import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/collection-navigation.module.css";

export default function CollectionsLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем подборки" className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Редакционный выбор</p>
          <h1>Загружаем подборки</h1>
        </div>
        <div aria-hidden="true" className={styles.state}>
          <p>Подготавливаем редакционный список…</p>
        </div>
      </Container>
    </main>
  );
}
