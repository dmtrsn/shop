import { Container } from "@/components/ui/container";
import { CartSkeleton } from "@/components/storefront/cart-view";
import styles from "@/components/storefront/cart.module.css";

export default function CheckoutLoading() {
  return (
    <main className={styles.main} id="main-content">
      <Container>
        <header className={styles.heading}><h1>Оформление заказа</h1></header>
        <CartSkeleton />
      </Container>
    </main>
  );
}
