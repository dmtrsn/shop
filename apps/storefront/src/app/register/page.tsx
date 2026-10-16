import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";

import cartStyles from "@/components/storefront/cart.module.css";
import styles from "@/components/storefront/login.module.css";
import { RegisterForm } from "@/components/storefront/register-form";
import { Container } from "@/components/ui/container";
import { getAuthCapabilities, getServerSession } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Регистрация — Слой", robots: { index: false, follow: false } };

// Self-registration. Go decides whether it exists here (selfService); the
// page never guesses, and an outage is not reported as "disabled".
export default async function RegisterPage() {
  const session = await getServerSession();
  if (session.status === "authenticated") redirect("/account");
  const capabilities = await getAuthCapabilities();
  return (
    <main className={cartStyles.main} id="main-content">
      <Container>
        <section aria-labelledby="register-title" className={styles.card}>
          <h1 id="register-title">Регистрация</h1>
          {capabilities.status === "unavailable" ? (
            <p className={styles.notice} role="status">Сервис регистрации временно недоступен. Попробуйте позже.</p>
          ) : capabilities.selfService ? (
            <>
              <p className={styles.lede}>Аккаунт появится после подтверждения адреса: мы пришлём ссылку на почту.</p>
              <RegisterForm />
            </>
          ) : (
            <p className={styles.notice} role="status">
              В этом магазине нельзя зарегистрироваться самостоятельно. Если у вас уже есть аккаунт, войдите.
            </p>
          )}
          {capabilities.status === "available" && capabilities.selfService ? null : (
            <p className={styles.links}><Link href="/login">Войти</Link></p>
          )}
        </section>
      </Container>
    </main>
  );
}
