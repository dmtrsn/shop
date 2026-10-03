import { Container } from "@/components/ui/container";
import { OrderSkeleton } from "@/components/storefront/order-view";
import styles from "@/components/storefront/cart.module.css";

export default function OrderLoading() {
  return (
    <main className={styles.main} id="main-content">
      <Container>
        <header className={styles.heading}><h1>Заказ</h1></header>
        <OrderSkeleton />
      </Container>
    </main>
  );
}
