import type { Metadata } from "next";

import cartStyles from "@/components/storefront/cart.module.css";
import styles from "@/components/storefront/login.module.css";
import { PasswordReset } from "@/components/storefront/password-reset";
import { Container } from "@/components/ui/container";

// Dynamic like every page here: the CSP nonce exists only per request.
export const dynamic = "force-dynamic";
export const metadata: Metadata = {
  title: "Новый пароль — Слой",
  robots: { index: false, follow: false },
  referrer: "no-referrer",
};

// The emailed link carries its token in the fragment, which never reaches
// this server component; the client component reads and clears it.
export default function ResetPasswordPage() {
  return (
    <main className={cartStyles.main} id="main-content">
      <Container>
        <section aria-labelledby="reset-title" className={styles.card}>
          <h1 id="reset-title">Новый пароль</h1>
          <p className={styles.lede}>После смены пароля все прежние входы будут завершены.</p>
          <PasswordReset />
        </section>
      </Container>
    </main>
  );
}
