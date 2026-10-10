"use client";

import { LegacyLoginLink } from "./legacy-login-link";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { logoutBrowserSession } from "@/lib/api/auth-client";
import type { RouteSession } from "@/lib/api/browser-session";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import styles from "./account.module.css";
import cartStyles from "./cart.module.css";

type Feedback = { generation: number; kind: "failed" | "signedOut" };

export function AccountView({ initialSession, loginHref }: { initialSession: RouteSession; loginHref: string | null }) {
  const store = useBrowserSessionStore();
  const session = useBrowserSession();
  const routeKey = initialSession.status === "authenticated" ? `user:${initialSession.userID}` : initialSession.status;
  const [enteredKey, setEnteredKey] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const pendingRef = useRef<object | null>(null);
  const routeRef = useRef<object | null>(null);
  const signedOutHeading = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    routeRef.current = {};
    pendingRef.current = null;
    setPending(false);
    void store.enterRoute(initialSession);
    setEnteredKey(routeKey);
    return () => { routeRef.current = null; };
  }, [store, initialSession, routeKey]);

  const entered = enteredKey === routeKey;
  const status = entered ? session.status : initialSession.status === "authenticated" ? "loading" : initialSession.status;
  const notice = entered && feedback?.generation === session.generation ? feedback.kind : null;
  useEffect(() => {
    if (status === "anonymous" && notice === "signedOut") signedOutHeading.current?.focus();
  }, [status, notice]);

  async function logout() {
    // Capture identity, CSRF and generation from one snapshot. A stale button
    // cannot start a write for a newly discovered user.
    const captured = store.get();
    if (!entered || pendingRef.current || captured !== session || captured.status !== "authenticated") return;
    const attempt = {};
    pendingRef.current = attempt;
    setPending(true);
    setFeedback(null);
    const route = routeRef.current;
    const active = () => route !== null && routeRef.current === route;
    const current = () => {
      const now = store.get();
      return active() && now.status === "authenticated" && now.userID === captured.userID && now.generation === captured.generation;
    };
    try {
      await logoutBrowserSession({ expectedUserID: captured.userID, csrfToken: captured.csrfToken });
      if (!current()) return;
      store.invalidate("anonymous");
      setFeedback({ generation: store.get().generation, kind: "signedOut" });
    } catch {
      if (!current()) return;
      // Every explicit failed attempt gets one reconciliation, including 401,
      // CSRF, unsafe owners and uncertain delivery. No destructive replay.
      // refresh hides the summary synchronously and shares any in-flight check.
      const recovery = store.refresh();
      const generation = store.get().generation;
      await recovery;
      const now = store.get();
      if (!active() || now.generation !== generation) return;
      setFeedback({ generation, kind: now.status === "anonymous" ? "signedOut" : "failed" });
    } finally {
      if (pendingRef.current === attempt) {
        pendingRef.current = null;
        if (active()) setPending(false);
      }
    }
  }

  let content;
  if (status === "anonymous") {
    content = <section className={cartStyles.state}>
      <h2 ref={signedOutHeading} tabIndex={-1}>Вы вышли из аккаунта</h2>
      <p>Войдите, чтобы увидеть данные аккаунта и свои покупки.</p>
      <div className={cartStyles.actions}>
        {loginHref ? <LegacyLoginLink href={loginHref}>Войти</LegacyLoginLink> : null}
        <Button variant="secondary" onClick={() => void store.refresh()}>Проверить вход</Button>
      </div>
    </section>;
  } else if (status === "unavailable") {
    content = <section className={cartStyles.state}>
      <h2>Аккаунт временно недоступен</h2>
      <p role="status">Не удалось проверить сессию. Вход или выход из аккаунта не подтверждён.</p>
      <Button variant="secondary" onClick={() => void store.refresh()}>Повторить проверку</Button>
    </section>;
  } else if (status === "authenticated" && session.status === "authenticated") {
    content = <div className={styles.account}>
      <dl className={styles.summary}>
        <div><dt>Имя</dt><dd>{session.account.name}</dd></div>
        <div><dt>Email</dt><dd>{session.account.email}</dd></div>
        {session.account.phone ? <div><dt>Телефон</dt><dd>{session.account.phone}</dd></div> : null}
      </dl>
      <nav aria-label="Разделы аккаунта" className={styles.links}>
        <Link href="/account/orders" prefetch={false}>Мои заказы</Link>
        <Link href="/favorites" prefetch={false}>Избранное</Link>
        <Link href="/cart" prefetch={false}>Корзина</Link>
      </nav>
      <div className={styles.logout}>
        <Button variant="secondary" disabled={pending} aria-busy={pending} onClick={() => void logout()}>
          {pending ? "Выходим…" : "Выйти"}
        </Button>
        <p role="status" aria-live="polite">{pending ? "Проверяем выход из аккаунта…" : notice === "failed"
          ? "Выход не подтверждён. Проверьте текущий аккаунт и нажмите «Выйти» ещё раз, если хотите выйти." : ""}</p>
      </div>
    </div>;
  } else content = <AccountSkeleton />;

  return <main className={cartStyles.main} id="main-content"><Container>
    <header className={cartStyles.heading}><h1>Аккаунт</h1></header>
    {content}
  </Container></main>;
}

export function AccountSkeleton() {
  return <div className={styles.skeleton} aria-busy="true" role="status">Проверяем аккаунт…</div>;
}
