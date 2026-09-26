import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/product-detail.module.css";

export default function ProductLoading() {
  return (
    <main aria-busy="true" className={styles.main} id="main-content">
      <Container>
        <p role="status">Загружаем товар…</p>
        <div aria-hidden="true" className={styles.loadingGrid}>
          <div className={styles.skeletonImage} />
          <div className={styles.skeletonInfo}>
            <div className={styles.skeletonLineShort} />
            <div className={styles.skeletonLine} />
            <div className={styles.skeletonLineShort} />
            <div className={styles.skeletonLine} />
            <div className={styles.skeletonControl} />
            <div className={styles.skeletonControl} />
          </div>
        </div>
      </Container>
    </main>
  );
}
