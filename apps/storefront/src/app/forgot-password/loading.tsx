import cartStyles from "@/components/storefront/cart.module.css";
import styles from "@/components/storefront/login.module.css";
import { Container } from "@/components/ui/container";

export default function Loading() {
  return <main className={cartStyles.main} id="main-content"><Container>
    <section className={styles.card} aria-busy="true">
      <h1>Восстановление пароля</h1>
      <p className={styles.lede} role="status">Загружаем…</p>
    </section>
  </Container></main>;
}
