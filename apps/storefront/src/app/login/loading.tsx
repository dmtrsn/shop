import styles from "@/components/storefront/login.module.css";
import cartStyles from "@/components/storefront/cart.module.css";
import { Container } from "@/components/ui/container";

export default function LoginLoading() {
  return <main className={cartStyles.main} id="main-content"><Container>
    <section className={styles.card} aria-busy="true">
      <h1>Вход в аккаунт</h1>
      <p className={styles.lede} role="status">Проверяем вход…</p>
    </section>
  </Container></main>;
}
