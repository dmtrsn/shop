"use client";

import Link from "next/link";
import { useEffect, useRef, useState, useSyncExternalStore, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/text-input";
import { registerCustomer, resendRegistration } from "@/lib/api/auth-client";
import { BrowserRequestError } from "@/lib/api/browser-request";

import styles from "./login.module.css";

type Field = "name" | "email" | "password" | "phone";
type FieldErrors = Partial<Record<Field, string>>;
type Failure = { message: string; fields: FieldErrors };

const fieldNames: readonly Field[] = ["name", "email", "password", "phone"];
const unavailable = "Сервис регистрации недоступен, попробуйте позже";
// Go allows one account email per address a minute.
const resendCooldownSeconds = 60;

// Only Go's own field messages are shown. No answer says anything about
// whether the address already has an account: Go never tells.
export function registrationFailure(error: unknown): Failure {
  if (error instanceof BrowserRequestError) {
    if (error.status === 422) {
      const fields: FieldErrors = {};
      for (const { field, message } of error.fields) {
        if ((fieldNames as readonly string[]).includes(field) && !fields[field as Field]) fields[field as Field] = message;
      }
      return { message: Object.keys(fields).length ? "" : "Проверьте заполненные поля", fields };
    }
    if (error.status === 403 && error.code === "csrf_failed") return { message: "Не удалось отправить: обновите страницу", fields: {} };
    if (error.status === 429) return { message: "Слишком много попыток. Повторите через минуту", fields: {} };
    if (error.status === 503) return { message: "Сервис перегружен, повторите через несколько секунд", fields: {} };
    if (error.status === 404) return { message: "Регистрация сейчас недоступна", fields: {} };
  }
  return { message: unavailable, fields: {} };
}

const noSubscription = () => () => {};

export function RegisterForm() {
  // Submit stays disabled until hydration, so a native submit can never send
  // the password anywhere; without JavaScript the form explains itself.
  const hydrated = useSyncExternalStore(noSubscription, () => true, () => false);
  const [values, setValues] = useState<Record<Field, string>>({ name: "", email: "", password: "", phone: "" });
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [sentTo, setSentTo] = useState<string | null>(null);
  const inFlight = useRef(false);
  const sentHeading = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    if (sentTo) sentHeading.current?.focus();
  }, [sentTo]);

  const update = (field: Field) => (event: { target: { value: string } }) => setValues((current) => ({ ...current, [field]: event.target.value }));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current) return;
    const missing: FieldErrors = {};
    if (!values.name.trim()) missing.name = "Укажите имя";
    if (!values.email.trim()) missing.email = "Укажите email";
    if (!values.password) missing.password = "Придумайте пароль";
    if (Object.keys(missing).length) {
      setFailure({ message: "", fields: missing });
      return;
    }
    inFlight.current = true;
    setPending(true);
    setFailure(null);
    const email = values.email.trim();
    try {
      await registerCustomer({ name: values.name.trim(), email, password: values.password, phone: values.phone.trim() });
    } catch (error) {
      setFailure(registrationFailure(error));
      return;
    } finally {
      inFlight.current = false;
      setPending(false);
    }
    // The password is not needed any more; drop it from memory.
    setValues((current) => ({ ...current, password: "" }));
    setSentTo(email);
  }

  if (sentTo) {
    return (
      <div className={styles.sent}>
        <h2 className={styles.sentTitle} ref={sentHeading} tabIndex={-1}>Проверьте почту</h2>
        <p role="status">
          Мы отправили письмо на <strong>{sentTo}</strong>. Откройте ссылку из письма и введите пароль, который
          вы только что придумали. Если аккаунт с этим адресом уже есть, в письме будет ссылка для входа.
        </p>
        <p className={styles.hint}>Ссылка действует 48 часов. Письмо может прийти не сразу; проверьте и папку «Спам».</p>
        <ResendRegistration email={sentTo} />
        <p className={styles.links}>
          <button className={styles.textButton} onClick={() => setSentTo(null)} type="button">Указать другой адрес</button>
          <Link href="/login">Войти</Link>
        </p>
      </div>
    );
  }

  const fields = failure?.fields ?? {};
  const describedBy = (field: Field, hint?: string) => [hint, fields[field] ? `register-${field}-error` : ""].filter(Boolean).join(" ") || undefined;
  return (
    <form className={styles.form} method="post" noValidate onSubmit={(event) => void submit(event)}>
      <TextInput aria-describedby={describedBy("name")} aria-invalid={fields.name ? true : undefined} autoComplete="name"
        id="register-name" label="Имя" name="name" onChange={update("name")} readOnly={pending} value={values.name} />
      {fields.name ? <p className={styles.fieldError} id="register-name-error">{fields.name}</p> : null}
      <TextInput aria-describedby={describedBy("email")} aria-invalid={fields.email ? true : undefined} autoComplete="email"
        id="register-email" inputMode="email" label="Email" name="email" onChange={update("email")} readOnly={pending} type="email" value={values.email} />
      {fields.email ? <p className={styles.fieldError} id="register-email-error">{fields.email}</p> : null}
      <TextInput aria-describedby={describedBy("password", "register-password-hint")} aria-invalid={fields.password ? true : undefined}
        autoComplete="new-password" id="register-password" label="Пароль" name="password" onChange={update("password")} readOnly={pending}
        type="password" value={values.password} />
      <p className={styles.hint} id="register-password-hint">Не короче 10 символов.</p>
      {fields.password ? <p className={styles.fieldError} id="register-password-error">{fields.password}</p> : null}
      <TextInput aria-describedby={describedBy("phone")} aria-invalid={fields.phone ? true : undefined} autoComplete="tel"
        id="register-phone" inputMode="tel" label="Телефон (необязательно)" name="phone" onChange={update("phone")} readOnly={pending} type="tel" value={values.phone} />
      {fields.phone ? <p className={styles.fieldError} id="register-phone-error">{fields.phone}</p> : null}
      {failure?.message ? <p className={styles.error} role="alert">{failure.message}</p> : null}
      <Button aria-busy={pending} className={styles.submit} disabled={!hydrated || pending} type="submit">
        {pending ? "Отправляем…" : "Создать аккаунт"}
      </Button>
      <noscript><p className={styles.hint}>Для регистрации включите JavaScript.</p></noscript>
      <p className={styles.links}>Уже есть аккаунт? <Link href="/login">Войти</Link></p>
    </form>
  );
}

// Resend asks Go for a new confirmation email. Go answers the same whatever
// happened, so the copy only says what is true in every case.
function ResendRegistration({ email }: { email: string }) {
  const [remaining, setRemaining] = useState(resendCooldownSeconds);
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState<{ text: string; error: boolean } | null>(null);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = setTimeout(() => setRemaining((value) => value - 1), 1000);
    return () => clearTimeout(timer);
  }, [remaining]);

  async function resend() {
    if (pending || remaining > 0) return;
    setPending(true);
    setMessage(null);
    try {
      await resendRegistration(email);
      setMessage({ text: "Если регистрация ждёт подтверждения, мы отправили новое письмо. Используйте ссылку из самого нового письма.", error: false });
      setRemaining(resendCooldownSeconds);
    } catch (error) {
      setMessage({ text: registrationFailure(error).message || unavailable, error: true });
    } finally {
      setPending(false);
    }
  }

  return (
    <div className={styles.resend}>
      <Button aria-busy={pending} disabled={pending || remaining > 0} onClick={() => void resend()} variant="secondary">
        {remaining > 0 ? `Отправить ещё раз можно через ${remaining} с` : pending ? "Отправляем…" : "Отправить письмо ещё раз"}
      </Button>
      {message ? <p className={message.error ? styles.error : styles.hint} role={message.error ? "alert" : "status"}>{message.text}</p> : null}
    </div>
  );
}
