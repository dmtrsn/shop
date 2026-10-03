import { Container } from "@/components/ui/container";
import { CartSkeleton } from "@/components/storefront/cart-view";
import styles from "@/components/storefront/cart.module.css";

export default function CartLoading() {
  return (
    <main className={styles.main} id="main-content">
      <Container>
        <header className={styles.heading}><h1>Корзина</h1></header>
        <CartSkeleton />
      </Container>
    </main>
  );
}
