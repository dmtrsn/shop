"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, type FormEvent } from "react";

import { loginStaff } from "@/lib/staff-api";
import styles from "./login.module.css";

export const loginMessages = {
  email: "Укажите email",
  password: "Укажите пароль",
  // One message for unknown accounts, wrong passwords and disabled accounts.
  invalid: "Неверный email или пароль.",
  rateLimited: "Слишком много попыток входа. Повторите позже.",
  unavailable: "Вход временно недоступен. Повторите попытку позже.",
} as const;

function rateLimitedMessage(seconds?: number): string {
  return seconds ? `Слишком много попыток входа. Повторите через ${seconds} с.` : loginMessages.rateLimited;
}

/**
 * Staff sign-in. The password lives only in this form's state until it is
 * sent; on success Go sets the HttpOnly staff cookie and the server gate takes
 * over. No result is stored in the browser.
 */
export function LoginForm() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [fieldErrors, setFieldErrors] = useState<{ email?: string; password?: string }>({});
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const errorRef = useRef<HTMLParagraphElement>(null);

  function showError(message: string) {
    setError(message);
    requestAnimationFrame(() => errorRef.current?.focus());
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    const errors = {
      ...(email.trim() ? {} : { email: loginMessages.email }),
      ...(password ? {} : { password: loginMessages.password }),
    };
    setFieldErrors(errors);
    setError(null);
    if (errors.email || errors.password) return;
    setPending(true);
    const result = await loginStaff(email.trim(), password);
    switch (result.status) {
      case "signed-in":
        setPassword("");
        router.replace("/");
        router.refresh();
        return;
      case "invalid":
        setPassword("");
        showError(loginMessages.invalid);
        break;
      case "rate-limited":
        showError(rateLimitedMessage(result.retryAfterSeconds));
        break;
      default:
        showError(loginMessages.unavailable);
    }
    setPending(false);
  }

  return (
    <form aria-busy={pending} className={styles.form} noValidate onSubmit={handleSubmit}>
      {error ? (
        <p className={styles.error} ref={errorRef} role="alert" tabIndex={-1}>
          {error}
        </p>
      ) : null}
      <div className={styles.field}>
        <label htmlFor="staff-email">Email</label>
        <input
          aria-describedby={fieldErrors.email ? "staff-email-error" : undefined}
          aria-invalid={fieldErrors.email ? true : undefined}
          autoCapitalize="none"
          autoComplete="email"
          id="staff-email"
          inputMode="email"
          name="email"
          onChange={(event) => setEmail(event.target.value)}
          spellCheck={false}
          type="email"
          value={email}
        />
        {fieldErrors.email ? (
          <p className={styles.fieldError} id="staff-email-error">
            {fieldErrors.email}
          </p>
        ) : null}
      </div>
      <div className={styles.field}>
        <label htmlFor="staff-password">Пароль</label>
        <input
          aria-describedby={fieldErrors.password ? "staff-password-error" : undefined}
          aria-invalid={fieldErrors.password ? true : undefined}
          autoComplete="current-password"
          id="staff-password"
          name="password"
          onChange={(event) => setPassword(event.target.value)}
          type="password"
          value={password}
        />
        {fieldErrors.password ? (
          <p className={styles.fieldError} id="staff-password-error">
            {fieldErrors.password}
          </p>
        ) : null}
      </div>
      <button className={styles.submit} disabled={pending} type="submit">
        {pending ? "Входим…" : "Войти"}
      </button>
    </form>
  );
}
