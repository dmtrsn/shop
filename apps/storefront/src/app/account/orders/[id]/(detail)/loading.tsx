import Link from "next/link";

import { OrderSkeleton } from "@/components/storefront/order-view";
import { Container } from "@/components/ui/container";
import { ArrowRightIcon } from "@/components/ui/icons";
import { PageHeader } from "@/components/ui/page-header";
import styles from "@/components/storefront/cart.module.css";
import ordersStyles from "@/components/storefront/orders.module.css";

// Lives in a route group so it wraps only this page: a first navigation into a
// nested route must not flash this page's header.
export default function OrderLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <Link className={ordersStyles.back} href="/account/orders" prefetch={false}><ArrowRightIcon aria-hidden="true" />Все заказы</Link>
    <PageHeader title="Заказ" />
    <OrderSkeleton />
  </Container></main>;
}
