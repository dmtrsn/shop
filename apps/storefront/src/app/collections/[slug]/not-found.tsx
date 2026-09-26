import Link from "next/link";

import { Container } from "@/components/ui/container";

import styles from "@/components/storefront/collection-navigation.module.css";

export default function CollectionNotFound() {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <div className={styles.state}>
          <h1>Подборка не найдена</h1>
          <p>Возможно, ссылка устарела или подборка сейчас недоступна.</p>
          <Link href="/collections">К списку подборок</Link>
        </div>
      </Container>
    </main>
  );
}
