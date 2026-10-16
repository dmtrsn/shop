import { AccountNav } from "@/components/storefront/account-nav";
import { AccountSkeleton } from "@/components/storefront/account-view";
import { Container } from "@/components/ui/container";
import { PageHeader } from "@/components/ui/page-header";
import accountStyles from "@/components/storefront/account.module.css";
import styles from "@/components/storefront/cart.module.css";

// Lives in a route group so it wraps only this page: a first navigation into a
// nested route must not flash this page's header.
// Same header and side navigation as the page, so nothing jumps when it renders.
export default function AccountLoading() {
  return <main className={styles.main} id="main-content"><Container>
    <PageHeader title="Аккаунт" subtitle="Личные данные и покупки" />
    <div className={accountStyles.layout}>
      <AccountNav current="account" />
      <div className={accountStyles.content}><AccountSkeleton /></div>
    </div>
  </Container></main>;
}
