import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/catalog.module.css";

export default function CatalogLoading() {
  return (
    <main aria-busy="true" aria-label="Загружаем каталог" className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.loadingHeading}>
          <span /><span /><span />
        </div>
        <div className={styles.loadingLayout}>
          <aside aria-hidden="true" className={styles.loadingRail} />
          <div className={styles.loadingGrid}>
            {Array.from({ length: 6 }, (_, index) => <span key={index} />)}
          </div>
        </div>
      </Container>
    </main>
  );
}
