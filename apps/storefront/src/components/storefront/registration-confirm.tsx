"use client";

import Link from "next/link";
import { useLayoutEffect, useRef, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/text-input";
import { confirmRegistration } from "@/lib/api/auth-client";
import { BrowserRequestError } from "@/lib/api/browser-request";
import { replaceDocument } from "@/lib/browser-navigation";
import { takeRegistrationToken } from "@/lib/registration-token";

import styles from "./login.module.css";

type Phase = "reading" | "missing" | "form" | "invalid";

const accountPath = "/account";
const confirmPath = "/register/confirm";

export function confirmationFailure(error: unknown): { phase: "form" | "invalid"; message: string } {
  if (error instanceof BrowserRequestError) {
    if (error.status === 410) return { phase: "invalid", message: "" };
    if (error.status === 401) return { phase: "form", message: "Пароль не совпадает с тем, что вы указали при регистрации" };
    if (error.status === 422) return { phase: "form", message: "Введите пароль" };
    if (error.status === 403 && error.code === "csrf_failed") return { phase: "form", message: "Не удалось подтвердить: обновите страницу" };
    if (error.status === 429) return { phase: "form", message: "Слишком много попыток. Повторите через минуту" };
    if (error.status === 503) return { phase: "form", message: "Сервис перегружен, повторите через несколько секунд" };
  }
  return { phase: "form", message: "Сервис регистрации недоступен, попробуйте позже" };
}

// The page renders at <basePath>/register/confirm; the account link needs it.
function browserAccountPath(): string {
  const { pathname } = window.location;
  const base = pathname.endsWith(confirmPath) ? pathname.slice(0, -confirmPath.length) : "";
  return base + accountPath;
}

export function RegistrationConfirm() {
  // The raw token lives only here, never in state that could be serialized,
  // storage, a URL or a log.
  const token = useRef<string | null | undefined>(undefined);
  const [phase, setPhase] = useState<Phase>("reading");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const inFlight = useRef(false);

  // Before the first paint: take the token and clear the fragment. A second
  // run (React's development double effects) keeps the token already taken.
  useLayoutEffect(() => {
    if (token.current === undefined) token.current = takeRegistrationToken();
    setPhase(token.current ? "form" : "missing");
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || !token.current) return;
    if (!password) {
      setMessage("Введите пароль");
      return;
    }
    inFlight.current = true;
    setPending(true);
    setMessage("");
    try {
      await confirmRegistration({ token: token.current, password });
    } catch (error) {
      inFlight.current = false;
      setPending(false);
      const failure = confirmationFailure(error);
      if (failure.phase === "invalid") {
        token.current = null;
        setPassword("");
      }
      setPhase(failure.phase);
      setMessage(failure.message);
      return;
    }
    token.current = null;
    // A full navigation discards every in-memory state of this document.
    replaceDocument(browserAccountPath());
  }

  if (phase === "reading") {
    return <p className={styles.lede} role="status">Проверяем ссылку…</p>;
  }
  if (phase === "missing" || phase === "invalid") {
    return (
      <div className={styles.sent} role={phase === "invalid" ? "alert" : undefined}>
        <h2 className={styles.sentTitle}>{phase === "invalid" ? "Ссылка недействительна" : "Ссылка неполная"}</h2>
        <p>
          {phase === "invalid"
            ? "Ссылка устарела, уже использована или заменена более новым письмом."
            : "Откройте ссылку из письма целиком: скопируйте её полностью или нажмите на неё в письме."}{" "}
          Если вы уже подтвердили регистрацию, просто войдите.
        </p>
        <p className={styles.links}>
          <Link href="/login">Войти</Link>
          <Link href="/register">Зарегистрироваться снова</Link>
        </p>
      </div>
    );
  }
  return (
    <form className={styles.form} method="post" noValidate onSubmit={(event) => void submit(event)}>
      <TextInput aria-describedby={message ? "confirm-password-error" : undefined} aria-invalid={message ? true : undefined}
        autoComplete="current-password" id="confirm-password" label="Пароль" name="password"
        onChange={(event) => setPassword(event.target.value)} readOnly={pending} type="password" value={password} />
      {message ? <p className={styles.error} id="confirm-password-error" role="alert">{message}</p> : null}
      <Button aria-busy={pending} className={styles.submit} disabled={pending} type="submit">
        {pending ? "Подтверждаем…" : "Подтвердить и войти"}
      </Button>
    </form>
  );
}
