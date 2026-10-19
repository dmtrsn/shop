import type { Metadata } from "next";
import Link from "next/link";

import cartStyles from "@/components/storefront/cart.module.css";
import { ForgotPasswordForm } from "@/components/storefront/forgot-password-form";
import styles from "@/components/storefront/login.module.css";
import { Container } from "@/components/ui/container";
import { getAuthCapabilities } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Восстановление пароля — Слой", robots: { index: false, follow: false } };

// Recovery exists exactly where self-service does (Go's selfService flag).
export default async function ForgotPasswordPage() {
  const capabilities = await getAuthCapabilities();
  return (
    <main className={cartStyles.main} id="main-content">
      <Container>
        <section aria-labelledby="forgot-title" className={styles.card}>
          <h1 id="forgot-title">Восстановление пароля</h1>
          {capabilities.status === "unavailable" ? (
            <p className={styles.notice} role="status">Сервис восстановления временно недоступен. Попробуйте позже.</p>
          ) : capabilities.selfService ? (
            <>
              <p className={styles.lede}>Укажите email аккаунта — пришлём ссылку для смены пароля.</p>
              <ForgotPasswordForm />
            </>
          ) : (
            <p className={styles.notice} role="status">В этом магазине нельзя восстановить пароль самостоятельно.</p>
          )}
          {capabilities.status === "available" && capabilities.selfService ? null : (
            <p className={styles.links}><Link href="/login">Войти</Link></p>
          )}
        </section>
      </Container>
    </main>
  );
}
