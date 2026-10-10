"use client";

import { useRef, useState, useSyncExternalStore, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/text-input";
import { startBrowserSession } from "@/lib/api/auth-client";
import { BrowserRequestError } from "@/lib/api/browser-request";
import { replaceDocument } from "@/lib/browser-navigation";
import { loginPath, loginReturnTarget } from "@/lib/return-to";

import styles from "./login.module.css";

type Field = "email" | "password";
type FieldErrors = Partial<Record<Field, string>>;
type Failure = { message: string; fields: FieldErrors };

const unavailable = "Сервис входа недоступен, попробуйте позже";

// Only Go's own field messages are shown; no other backend detail reaches the UI.
export function loginFailure(error: unknown): Failure {
  if (error instanceof BrowserRequestError) {
    if (error.status === 401) return { message: "Неверный email или пароль", fields: {} };
    if (error.status === 403 && error.code === "csrf_failed") return { message: "Не удалось войти: обновите страницу", fields: {} };
    if (error.status === 422) {
      const fields: FieldErrors = {};
      for (const { field, message } of error.fields) {
        if ((field === "email" || field === "password") && !fields[field]) fields[field] = message;
      }
      return { message: Object.keys(fields).length ? "" : "Проверьте email и пароль", fields };
    }
  }
  return { message: unavailable, fields: {} };
}

// The page renders at <basePath>/login and passes an app path; the browser
// navigation needs the basePath back. Root deployments have none.
function browserTarget(target: string): string {
  const { pathname } = window.location;
  const base = pathname.endsWith(loginPath) ? pathname.slice(0, -loginPath.length) : "";
  const path = loginReturnTarget(target);
  return base && path === "/" ? base : base + path;
}

const noSubscription = () => () => {};

export function LoginForm({ target }: { target: string }) {
  // Submit stays disabled until hydration, so a native submit can never send
  // the credentials anywhere; without JavaScript the form explains itself.
  const hydrated = useSyncExternalStore(noSubscription, () => true, () => false);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const inFlight = useRef(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;
    const missing: FieldErrors = {};
    if (!email.trim()) missing.email = "Укажите email";
    if (!password) missing.password = "Укажите пароль";
    if (missing.email || missing.password) {
      setFailure({ message: "", fields: missing });
      return;
    }
    inFlight.current = true;
    setPending(true);
    setFailure(null);
    try {
      // One POST; the transport never retries and the response body is dropped.
      await startBrowserSession({ email: email.trim(), password });
    } catch (error) {
      inFlight.current = false;
      setPending(false);
      setFailure(loginFailure(error));
      return;
    }
    // A full navigation, not a client transition: every in-memory anonymous
    // session, Cart and Favorites generation is discarded with the document.
    replaceDocument(browserTarget(target));
  }

  const fields = failure?.fields ?? {};
  return (
    <form className={styles.form} method="post" noValidate onSubmit={(event) => void submit(event)}>
      <TextInput
        aria-describedby={fields.email ? "login-email-error" : undefined}
        aria-invalid={fields.email ? true : undefined}
        autoComplete="email"
        id="login-email"
        inputMode="email"
        label="Email"
        name="email"
        onChange={(event) => setEmail(event.target.value)}
        readOnly={pending}
        type="email"
        value={email}
      />
      {fields.email ? <p className={styles.fieldError} id="login-email-error">{fields.email}</p> : null}
      <TextInput
        aria-describedby={fields.password ? "login-password-error" : undefined}
        aria-invalid={fields.password ? true : undefined}
        autoComplete="current-password"
        id="login-password"
        label="Пароль"
        name="password"
        onChange={(event) => setPassword(event.target.value)}
        readOnly={pending}
        type="password"
        value={password}
      />
      {fields.password ? <p className={styles.fieldError} id="login-password-error">{fields.password}</p> : null}
      {failure?.message ? <p className={styles.error} role="alert">{failure.message}</p> : null}
      <Button aria-busy={pending} className={styles.submit} disabled={!hydrated || pending} type="submit">
        {pending ? "Входим…" : "Войти"}
      </Button>
      <noscript><p className={styles.hint}>Для входа включите JavaScript.</p></noscript>
    </form>
  );
}
