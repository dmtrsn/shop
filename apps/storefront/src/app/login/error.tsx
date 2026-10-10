"use client";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import styles from "@/components/storefront/login.module.css";
import cartStyles from "@/components/storefront/cart.module.css";

export default function LoginError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <main className={cartStyles.main} id="main-content"><Container>
    <section className={styles.card} role="alert">
      <h1>Вход в аккаунт</h1>
      <p className={styles.lede}>Не удалось открыть страницу входа. Попробуйте ещё раз.</p>
      <Button variant="secondary" onClick={reset}>Повторить</Button>
    </section>
  </Container></main>;
}
