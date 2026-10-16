import { SkeletonRows } from "@/components/storefront/route-states";
import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/producer-navigation.module.css";

export default function ManufacturersLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем производителей" className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Производители</p>
          <h1>Загружаем справочник</h1>
        </div>
        <SkeletonRows />
      </Container>
    </main>
  );
}
