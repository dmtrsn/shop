"use client";

import { LoginLink } from "./login-link";

import { useEffect, useRef, useState } from "react";

import { Button, buttonClassName } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { EmptyState } from "@/components/ui/empty-state";
import { InfoIcon, UserIcon } from "@/components/ui/icons";
import { PageHeader } from "@/components/ui/page-header";
import { logoutBrowserSession } from "@/lib/api/auth-client";
import type { RouteSession } from "@/lib/api/browser-session";
import { initials } from "@/lib/initials";

import { AccountNav } from "./account-nav";
import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import styles from "./account.module.css";
import cartStyles from "./cart.module.css";

type Feedback = { generation: number; kind: "failed" | "signedOut" };

export function AccountView({ initialSession }: { initialSession: RouteSession }) {
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
    content = <EmptyState
      actions={<>
        <LoginLink className={buttonClassName("primary")}>Войти</LoginLink>
        <Button variant="secondary" onClick={() => void store.refresh()}>Проверить вход</Button>
      </>}
      icon={<UserIcon />}
      title="Вы вышли из аккаунта"
      titleProps={{ ref: signedOutHeading, tabIndex: -1 }}
    >
      Войдите, чтобы увидеть данные аккаунта и свои покупки.
    </EmptyState>;
  } else if (status === "unavailable") {
    // A failure, not a signed-out state: a solid notice instead of the dashed empty state.
    content = <section className={cartStyles.unavailableState}>
      <InfoIcon aria-hidden="true" />
      <div>
        <h2>Аккаунт временно недоступен</h2>
        <p role="status">Не удалось проверить сессию. Вход или выход из аккаунта не подтверждён.</p>
        <Button variant="secondary" onClick={() => void store.refresh()}>Повторить проверку</Button>
      </div>
    </section>;
  } else if (status === "authenticated" && session.status === "authenticated") {
    const letters = initials(session.account.name);
    content = <div className={styles.cards}>
      <section aria-labelledby="account-profile-title" className={styles.card}>
        <div className={styles.cardHead}>
          {/* The name is the textual identity in the list below; the avatar only repeats it. */}
          {letters ? <span aria-hidden="true" className={styles.avatar}>{letters}</span> : null}
          <h2 id="account-profile-title">Личные данные</h2>
        </div>
        <dl className={styles.summary}>
          <div><dt>Имя</dt><dd>{session.account.name}</dd></div>
          <div><dt>Email</dt><dd>{session.account.email}</dd></div>
          {session.account.phone ? <div><dt>Телефон</dt><dd>{session.account.phone}</dd></div> : null}
        </dl>
      </section>
      <section aria-labelledby="account-session-title" className={`${styles.card} ${styles.logout}`}>
        <h2 id="account-session-title">Выход</h2>
        <p className={styles.cardText}>Завершить сеанс в этом браузере.</p>
        <Button className={styles.logoutButton} variant="secondary" disabled={pending} aria-busy={pending} onClick={() => void logout()}>
          {pending ? "Выходим…" : "Выйти"}
        </Button>
        <p className={styles.logoutStatus} role="status" aria-live="polite">{pending ? "Проверяем выход из аккаунта…" : notice === "failed"
          ? "Выход не подтверждён. Проверьте текущий аккаунт и нажмите «Выйти» ещё раз, если хотите выйти." : ""}</p>
      </section>
    </div>;
  } else content = <AccountSkeleton />;

  return <main className={cartStyles.main} id="main-content"><Container>
    <PageHeader title="Аккаунт" subtitle="Личные данные и покупки" />
    <div className={styles.layout}>
      <AccountNav current="account" />
      <div className={styles.content}>{content}</div>
    </div>
  </Container></main>;
}

export function AccountSkeleton() {
  return <div className={styles.skeleton} aria-busy="true" role="status">Проверяем аккаунт…</div>;
}
