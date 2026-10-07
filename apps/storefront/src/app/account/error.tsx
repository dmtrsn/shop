"use client";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import styles from "@/components/storefront/cart.module.css";

export default function AccountError({ retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return <main className={styles.main} id="main-content"><Container>
    <header className={styles.heading}><h1>Аккаунт</h1></header>
    <section className={styles.state} role="alert">
      <h2>Не удалось открыть аккаунт</h2>
      <p>Попробуйте загрузить страницу ещё раз.</p>
      <Button variant="secondary" onClick={retry}>Повторить</Button>
    </section>
  </Container></main>;
}
