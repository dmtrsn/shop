import { Container } from "@/components/ui/container";
import { PageHeader } from "@/components/ui/page-header";
import { SkeletonRows } from "@/components/storefront/route-states";

import styles from "@/components/storefront/quick-order.module.css";

export default function Loading() {
  return <main aria-busy="true" aria-label="Загружаем быстрый заказ" className={styles.main} id="main-content">
    <Container><PageHeader title="Быстрый заказ" subtitle="Поиск товаров по производителям" /><SkeletonRows /></Container>
  </main>;
}
