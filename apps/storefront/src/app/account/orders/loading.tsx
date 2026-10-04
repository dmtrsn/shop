import { Container } from "@/components/ui/container";
import { OrdersSkeleton } from "@/components/storefront/orders-view";
import styles from "@/components/storefront/cart.module.css";

export default function OrdersLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <header className={styles.heading}><h1>Мои заказы</h1></header>
    <OrdersSkeleton />
  </Container></main>;
}
