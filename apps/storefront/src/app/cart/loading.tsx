import { CartSkeleton } from "@/components/storefront/cart-view";
import { Container } from "@/components/ui/container";
import { PageHeader } from "@/components/ui/page-header";
import styles from "@/components/storefront/cart.module.css";

export default function CartLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <PageHeader title="Корзина" />
    <CartSkeleton />
  </Container></main>;
}
