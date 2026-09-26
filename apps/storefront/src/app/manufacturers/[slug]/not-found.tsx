import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/producer-navigation.module.css";

export default function ManufacturerNotFound() {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <div className={styles.pageEmpty}>
          <h1>Производитель не найден</h1>
          <p>Возможно, ссылка устарела или производитель больше не опубликован.</p>
          <Link href="/manufacturers">К списку производителей</Link>
        </div>
      </Container>
    </main>
  );
}
