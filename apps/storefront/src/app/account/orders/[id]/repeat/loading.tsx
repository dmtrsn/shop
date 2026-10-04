import { Container } from "@/components/ui/container";
import { RepeatSkeleton } from "@/components/storefront/repeat-view";
import styles from "@/components/storefront/cart.module.css";
export default function RepeatLoading() {
  return <main className={styles.main} id="main-content"><Container><header className={styles.heading}><h1>Повторить заказ</h1></header><RepeatSkeleton /></Container></main>;
}
