"use client";

import Link from "next/link";
import { useLayoutEffect, useRef, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/text-input";
import { confirmPasswordReset } from "@/lib/api/auth-client";
import { BrowserRequestError } from "@/lib/api/browser-request";
import { takeFragmentToken } from "@/lib/fragment-token";

import styles from "./login.module.css";

type Phase = "reading" | "missing" | "form" | "invalid" | "done";

export function passwordResetFailure(error: unknown): { phase: "form" | "invalid"; message: string } {
  if (error instanceof BrowserRequestError) {
    if (error.status === 410) return { phase: "invalid", message: "" };
    if (error.status === 422) {
      return { phase: "form", message: error.fields.find((field) => field.field === "password")?.message || "Проверьте новый пароль" };
    }
    if (error.status === 403 && error.code === "csrf_failed") return { phase: "form", message: "Не удалось сменить пароль: обновите страницу" };
    if (error.status === 429) return { phase: "form", message: "Слишком много попыток. Повторите через минуту" };
    if (error.status === 503) return { phase: "form", message: "Сервис перегружен, повторите через несколько секунд" };
  }
  return { phase: "form", message: "Сервис восстановления недоступен, попробуйте позже" };
}

export function PasswordReset() {
  // The raw token lives only here: never in storage, a URL, props or logs.
  const token = useRef<string | null | undefined>(undefined);
  const [phase, setPhase] = useState<Phase>("reading");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const inFlight = useRef(false);
  const doneHeading = useRef<HTMLHeadingElement>(null);

  // Before the first paint: take the token and clear the fragment; a second
  // run (React's development double effects) keeps the token already taken.
  useLayoutEffect(() => {
    if (token.current === undefined) token.current = takeFragmentToken();
    setPhase(token.current ? "form" : "missing");
  }, []);

  useLayoutEffect(() => {
    if (phase === "done") doneHeading.current?.focus();
  }, [phase]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || !token.current) return;
    if (!password) {
      setMessage("Придумайте новый пароль");
      return;
    }
    inFlight.current = true;
    setPending(true);
    setMessage("");
    try {
      await confirmPasswordReset({ token: token.current, password });
    } catch (error) {
      const failure = passwordResetFailure(error);
      if (failure.phase === "invalid") token.current = null;
      setPhase(failure.phase);
      setMessage(failure.message);
      return;
    } finally {
      inFlight.current = false;
      setPending(false);
    }
    // Spent: nothing of the token or password stays in memory.
    token.current = null;
    setPassword("");
    setPhase("done");
  }

  if (phase === "reading") return <p className={styles.lede} role="status">Проверяем ссылку…</p>;
  if (phase === "done") {
    return (
      <div className={styles.sent}>
        <h2 className={styles.sentTitle} ref={doneHeading} tabIndex={-1}>Пароль изменён</h2>
        <p role="status">Все прежние входы завершены, в том числе на этом устройстве. Войдите с новым паролем.</p>
        <p className={styles.links}><Link href="/login">Войти</Link></p>
      </div>
    );
  }
  if (phase === "missing" || phase === "invalid") {
    return (
      <div className={styles.sent} role={phase === "invalid" ? "alert" : undefined}>
        <h2 className={styles.sentTitle}>{phase === "invalid" ? "Ссылка недействительна" : "Ссылка неполная"}</h2>
        <p>
          {phase === "invalid"
            ? "Ссылка устарела, уже использована или заменена более новым письмом."
            : "Откройте ссылку из письма целиком: скопируйте её полностью или нажмите на неё в письме."}{" "}
          Запросите новую ссылку.
        </p>
        <p className={styles.links}>
          <Link href="/forgot-password">Запросить новую ссылку</Link>
          <Link href="/login">Войти</Link>
        </p>
      </div>
    );
  }
  return (
    <form className={styles.form} method="post" noValidate onSubmit={(event) => void submit(event)}>
      <TextInput aria-describedby={["reset-password-hint", message ? "reset-password-error" : ""].filter(Boolean).join(" ")}
        aria-invalid={message ? true : undefined} autoComplete="new-password" id="reset-password" label="Новый пароль" name="password"
        onChange={(event) => setPassword(event.target.value)} readOnly={pending} type="password" value={password} />
      <p className={styles.hint} id="reset-password-hint">Не короче 10 символов.</p>
      {message ? <p className={styles.error} id="reset-password-error" role="alert">{message}</p> : null}
      <Button aria-busy={pending} className={styles.submit} disabled={pending} type="submit">
        {pending ? "Сохраняем…" : "Сменить пароль"}
      </Button>
    </form>
  );
}
