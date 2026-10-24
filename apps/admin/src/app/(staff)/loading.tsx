import styles from "@/components/overview.module.css";

export default function Loading() {
  return (
    <p aria-live="polite" className={styles.loading} role="status">
      Загрузка…
    </p>
  );
}
