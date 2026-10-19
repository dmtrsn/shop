"use client";

import Link from "next/link";
import { useEffect, useRef, useState, useSyncExternalStore, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/text-input";
import { requestPasswordReset } from "@/lib/api/auth-client";
import { BrowserRequestError } from "@/lib/api/browser-request";

import styles from "./login.module.css";

type Failure = { message: string; field?: string };

// Go answers every valid address alike; failures say nothing about accounts.
export function resetRequestFailure(error: unknown): Failure {
  if (error instanceof BrowserRequestError) {
    if (error.status === 422) {
      const field = error.fields.find((item) => item.field === "email")?.message;
      return field ? { message: "", field } : { message: "Проверьте email" };
    }
    if (error.status === 403 && error.code === "csrf_failed") return { message: "Не удалось отправить: обновите страницу" };
    if (error.status === 429) return { message: "Слишком много запросов. Повторите через минуту" };
    if (error.status === 404) return { message: "Восстановление пароля сейчас недоступно" };
  }
  return { message: "Сервис восстановления недоступен, попробуйте позже" };
}

const noSubscription = () => () => {};

export function ForgotPasswordForm() {
  const hydrated = useSyncExternalStore(noSubscription, () => true, () => false);
  const [address, setAddress] = useState("");
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [sent, setSent] = useState(false);
  const inFlight = useRef(false);
  const sentHeading = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    if (sent) sentHeading.current?.focus();
  }, [sent]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;
    if (!address.trim()) {
      setFailure({ message: "", field: "Укажите email" });
      return;
    }
    inFlight.current = true;
    setPending(true);
    setFailure(null);
    try {
      await requestPasswordReset(address.trim());
    } catch (error) {
      setFailure(resetRequestFailure(error));
      return;
    } finally {
      inFlight.current = false;
      setPending(false);
    }
    setSent(true);
  }

  if (sent) {
    return (
      <div className={styles.sent}>
        <h2 className={styles.sentTitle} ref={sentHeading} tabIndex={-1}>Проверьте почту</h2>
        <p role="status">
          Если аккаунт с адресом <strong>{address.trim()}</strong> существует, мы отправили на него письмо со ссылкой
          для смены пароля. Ссылка действует 1 час и только один раз.
        </p>
        <p className={styles.hint}>Письмо может прийти не сразу; проверьте и папку «Спам». Новый запрос можно отправить через минуту.</p>
        <p className={styles.links}>
          <button className={styles.textButton} onClick={() => setSent(false)} type="button">Отправить ещё раз</button>
          <Link href="/login">Войти</Link>
        </p>
      </div>
    );
  }

  return (
    <form className={styles.form} method="post" noValidate onSubmit={(event) => void submit(event)}>
      <TextInput aria-describedby={failure?.field ? "forgot-email-error" : undefined} aria-invalid={failure?.field ? true : undefined}
        autoComplete="email" id="forgot-email" inputMode="email" label="Email" name="email"
        onChange={(event) => setAddress(event.target.value)} readOnly={pending} type="email" value={address} />
      {failure?.field ? <p className={styles.fieldError} id="forgot-email-error">{failure.field}</p> : null}
      {failure?.message ? <p className={styles.error} role="alert">{failure.message}</p> : null}
      <Button aria-busy={pending} className={styles.submit} disabled={!hydrated || pending} type="submit">
        {pending ? "Отправляем…" : "Отправить ссылку"}
      </Button>
      <noscript><p className={styles.hint}>Для восстановления пароля включите JavaScript.</p></noscript>
      <p className={styles.links}>Вспомнили пароль? <Link href="/login">Войти</Link></p>
    </form>
  );
}
