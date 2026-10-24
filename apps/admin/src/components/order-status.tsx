import type { AdminOrder } from "@/lib/orders-api";
import styles from "./admin-pages.module.css";

// The label is the one Go stored; the badge style only separates finished
// orders (delivered, cancelled) from open ones, and the text always says which.
export function OrderStatusBadge({ order }: { order: Pick<AdminOrder, "status" | "statusLabel"> }) {
  const finished = order.status === "delivered" || order.status === "cancelled";
  return <span className={finished ? styles.statusArchived : styles.statusActive}>{order.statusLabel || order.status}</span>;
}

export function formatDateTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
}
