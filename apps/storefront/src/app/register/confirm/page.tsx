import type { Metadata } from "next";

import cartStyles from "@/components/storefront/cart.module.css";
import styles from "@/components/storefront/login.module.css";
import { RegistrationConfirm } from "@/components/storefront/registration-confirm";
import { Container } from "@/components/ui/container";

// Dynamic like every page here: the CSP nonce exists only per request.
export const dynamic = "force-dynamic";
export const metadata: Metadata = {
  title: "Подтверждение регистрации — Слой",
  robots: { index: false, follow: false },
  referrer: "no-referrer",
};

// The emailed link carries its token in the fragment, which never reaches
// this server component; the client component reads and clears it.
export default function RegisterConfirmPage() {
  return (
    <main className={cartStyles.main} id="main-content">
      <Container>
        <section aria-labelledby="register-confirm-title" className={styles.card}>
          <h1 id="register-confirm-title">Подтверждение регистрации</h1>
          <p className={styles.lede}>Введите пароль, который вы указали при регистрации. После этого аккаунт будет создан и вы войдёте в него.</p>
          <RegistrationConfirm />
        </section>
      </Container>
    </main>
  );
}
