import type { Metadata } from "next";
import Link from "next/link";

import styles from "@/components/overview.module.css";

export const metadata: Metadata = { title: "Обзор" };

// A truthful landing page: no metrics exist yet, so none are shown.
export default function OverviewPage() {
  return (
    <section aria-labelledby="overview-title" className={styles.overview}>
      <h1 id="overview-title">Панель управления</h1>
      <p>
        Вы вошли в панель управления магазином. В разделе <Link href="/products">Товары</Link> можно искать,
        создавать, редактировать, архивировать и восстанавливать товары. Управление заказами появится
        позже.
      </p>
    </section>
  );
}
