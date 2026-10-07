import { AccountSkeleton } from "@/components/storefront/account-view";
import { Container } from "@/components/ui/container";
import styles from "@/components/storefront/cart.module.css";

export default function AccountLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <header className={styles.heading}><h1>Аккаунт</h1></header>
    <AccountSkeleton />
  </Container></main>;
}
