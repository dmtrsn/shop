import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { LoginForm } from "@/components/login-form";
import styles from "@/components/login.module.css";
import { getServerStaffSession } from "@/lib/server-session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Вход" };

// Only a session Go confirms skips the form; an unreachable Go never counts
// as signed in or signed out.
export default async function LoginPage() {
  const session = await getServerStaffSession();
  if (session.status === "authenticated") redirect("/");
  return (
    <main className={styles.page} id="main-content">
      <section aria-labelledby="login-title" className={styles.card}>
        <p className={styles.brand}>
          Слой <span>Администрирование</span>
        </p>
        <h1 id="login-title">Вход для сотрудников</h1>
        {session.status === "unavailable" ? (
          <p className={styles.notice} role="status">
            Не удалось проверить текущий вход. Вы можете войти заново.
          </p>
        ) : null}
        <LoginForm />
        <p className={styles.hint}>Учётные записи сотрудников выдаёт администратор системы.</p>
      </section>
    </main>
  );
}
