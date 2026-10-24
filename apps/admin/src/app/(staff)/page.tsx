import type { Metadata } from "next";

import styles from "@/components/overview.module.css";

export const metadata: Metadata = { title: "Обзор" };

// A truthful landing page: no metrics exist yet, so none are shown.
export default function OverviewPage() {
  return (
    <section aria-labelledby="overview-title" className={styles.overview}>
      <h1 id="overview-title">Панель управления</h1>
      <p>
        Вы вошли в панель управления магазином. Управление каталогом и заказами появится здесь в
        следующих версиях.
      </p>
    </section>
  );
}
