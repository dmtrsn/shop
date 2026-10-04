"use client";
import Link from "next/link";
import { Container } from "@/components/ui/container";
import styles from "@/components/storefront/cart.module.css";
export default function RepeatError() {
  return <main className={styles.main} id="main-content"><Container><div className={styles.state} role="alert">
    <h1>Не удалось открыть повтор заказа</h1><p>Если добавление уже отправлено, его результат может быть неизвестен. Не отправляйте тот же повтор заново, пока не уточните результат.</p>
    <Link href="/account/orders" prefetch={false}>Вернуться к заказам</Link>
  </div></Container></main>;
}
