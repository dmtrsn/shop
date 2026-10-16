import Link from "next/link";

import { RepeatSkeleton } from "@/components/storefront/repeat-view";
import { Container } from "@/components/ui/container";
import { ArrowRightIcon } from "@/components/ui/icons";
import { PageHeader } from "@/components/ui/page-header";
import styles from "@/components/storefront/cart.module.css";
import ordersStyles from "@/components/storefront/orders.module.css";

export default function RepeatLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <Link className={ordersStyles.back} href="/account/orders" prefetch={false}><ArrowRightIcon aria-hidden="true" />Все заказы</Link>
    <PageHeader title="Повторить заказ" subtitle="Наличие и цены проверяются заново; товары добавляются к корзине" />
    <RepeatSkeleton />
  </Container></main>;
}
