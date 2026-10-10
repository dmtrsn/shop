import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { LoginForm } from "@/components/storefront/login-form";
import styles from "@/components/storefront/login.module.css";
import cartStyles from "@/components/storefront/cart.module.css";
import { Container } from "@/components/ui/container";
import { getDemoLoginAllowed, getServerSession } from "@/lib/api/session";
import { loginReturnTarget } from "@/lib/return-to";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Вход — Слой", robots: { index: false, follow: false } };

type LoginPageProps = { searchParams: Promise<Record<string, string | string[] | undefined>> };

// Server gate for Go's cookie login. Only the normalized app path reaches the
// browser; the raw returnTo never does.
export default async function LoginPage({ searchParams }: LoginPageProps) {
  const target = loginReturnTarget((await searchParams).returnTo);
  const session = await getServerSession();
  // redirect() adds any basePath. Only a confirmed session skips the form.
  if (session.status === "authenticated") redirect(target);
  const demoLogin = await getDemoLoginAllowed();
  return (
    <main className={cartStyles.main} id="main-content">
      <Container>
        <section aria-labelledby="login-title" className={styles.card}>
          <h1 id="login-title">Вход в аккаунт</h1>
          <p className={styles.lede}>Корзина, избранное и заказы доступны после входа.</p>
          {session.status === "unavailable" ? (
            <p className={styles.notice} role="status">
              Не удалось проверить текущий вход. Вы можете войти: после входа откроется нужная страница.
            </p>
          ) : null}
          <LoginForm target={target} />
          {/* Shown only when Go explicitly advertises its seeded demo account. */}
          {demoLogin ? (
            <p className={styles.demo}>
              Демо-режим. Тестовый аккаунт: <strong>demo@sugar.ru</strong>, пароль <strong>Sweet2026!</strong>
            </p>
          ) : null}
        </section>
      </Container>
    </main>
  );
}
