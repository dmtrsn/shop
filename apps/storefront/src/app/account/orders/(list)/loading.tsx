import { AccountNav } from "@/components/storefront/account-nav";
import { OrdersSkeleton } from "@/components/storefront/orders-view";
import { Container } from "@/components/ui/container";
import { PageHeader } from "@/components/ui/page-header";
import accountStyles from "@/components/storefront/account.module.css";
import styles from "@/components/storefront/cart.module.css";

// Lives in a route group so it wraps only this page: a first navigation into a
// nested route must not flash this page's header.
// Same header and side navigation as the page, so nothing jumps when it renders.
export default function OrdersLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <PageHeader title="Мои заказы" subtitle="История покупок и повтор заказа с проверкой текущего наличия" />
    <div className={accountStyles.layout}>
      <AccountNav current="orders" />
      <div className={accountStyles.content}><OrdersSkeleton /></div>
    </div>
  </Container></main>;
}
