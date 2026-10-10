"use client";

import { LegacyLoginLink } from "./legacy-login-link";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { BrowserRequestError, ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import { previewBrowserRepeat } from "@/lib/api/repeat-client";
import type { RepeatPreview } from "@/lib/api/repeat";
import { additionError, canRepeat, repeatBody, repeatDrafts, type RepeatDrafts } from "@/lib/repeat-review";
import { formatMoney } from "@/lib/money";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import { useCart, type RepeatAttempt } from "./cart-provider";
import cartStyles from "./cart.module.css";
import styles from "./repeat.module.css";

const statuses = { available: "Доступен", limited: "Доступен с ограничениями", unavailable: "Недоступен для добавления", discontinued: "Снят с продажи" };
type Review = { preview: RepeatPreview; drafts: RepeatDrafts; key: string };
type Phase = "idle" | "submitting" | "unknown" | "checking" | "added" | "stopped";

export function RepeatSkeleton() {
  return <p role="status" aria-busy="true">Загружаем условия повтора…</p>;
}

// Keep the same user's operation alive during a transient session check, but
// hide its entire DOM. A settled other identity unmounts and discards it.
export function RepeatView({ initialSession, orderID }: { initialSession: RouteSession; orderID: string }) {
  const store = useBrowserSessionStore();
  const session = useBrowserSession();
  const { beginRepeatReview, loginHref } = useCart();
  const routeKey = `${initialSession.status === "authenticated" ? initialSession.userID : initialSession.status}:${orderID}`;
  const [entered, setEntered] = useState("");
  const [owner, setOwner] = useState<string | null>(null);
  useEffect(() => {
    const release = beginRepeatReview();
    void store.enterRoute(initialSession);
    setEntered(routeKey);
    return release;
  }, [beginRepeatReview, initialSession, orderID, routeKey, store]);
  useEffect(() => {
    if (session.status === "authenticated") setOwner(session.userID);
    else if (session.status === "anonymous" || session.status === "unavailable") setOwner(null);
  }, [session]);
  const status = entered === routeKey ? session.status : initialSession.status === "authenticated" ? "loading" : initialSession.status;
  const userID = status === "authenticated" && session.status === "authenticated" ? session.userID
    : status === "loading" ? owner : null;
  return <main className={cartStyles.main} id="main-content"><Container>
    <Link className={cartStyles.secondaryLink} href="/account/orders" prefetch={false}>Все заказы</Link>
    <header className={cartStyles.heading}><h1>Повторить заказ</h1></header>
    {status === "anonymous" ? <section className={cartStyles.state}>
      <h2>Войдите, чтобы повторить заказ</h2>
      {loginHref ? <LegacyLoginLink href={loginHref}>Войти</LegacyLoginLink> : null}
      <Button onClick={() => void store.refresh()} variant="secondary">Проверить вход</Button>
    </section> : status === "unavailable" ? <section className={cartStyles.state}>
      <h2>Повтор заказа временно недоступен</h2>
      <Button onClick={() => void store.refresh()} variant="secondary">Повторить загрузку</Button>
    </section> : null}
    {status === "loading" || status === "idle" ? <RepeatSkeleton /> : null}
    {userID ? <div hidden={status !== "authenticated"}>
      <RepeatReview key={`${userID}:${orderID}`} orderID={orderID} userID={userID} />
    </div> : null}
  </Container></main>;
}

function RepeatReview({ userID, orderID }: { userID: string; orderID: string }) {
  const store = useBrowserSessionStore();
  const session = useBrowserSession();
  const cart = useCart();
  const router = useRouter();
  const [review, setReview] = useState<Review | null>(null);
  const [revision, setRevision] = useState(0);
  const [failure, setFailure] = useState<{ key: string; status: "failed" | "notFound" } | null>(null);
  const [phase, setPhase] = useState<Phase>("idle");
  const [notice, setNotice] = useState("");
  const [errors, setErrors] = useState<Record<number, string>>({});
  const [focus, setFocus] = useState<string | null>(null);
  const frozen = useRef<RepeatAttempt | null>(null);
  const inFlight = useRef(false);
  const alive = useRef(true);
  const latest = useRef<Review | null>(null);
  const csrfRecovery = useRef(false);
  const requestKey = `${session.generation}:${revision}`;
  const active = session.status === "authenticated" && session.userID === userID;
  useEffect(() => { alive.current = true; return () => { alive.current = false; frozen.current = null; }; }, []);
  useEffect(() => { latest.current = review; }, [review]);

  useEffect(() => {
    if (!active || phase !== "idle" || review?.key === requestKey || failure?.key === requestKey) return;
    const shared = store.get();
    if (shared.status !== "authenticated" || shared.userID !== userID) return;
    const controller = new AbortController();
    let currentRequest = true;
    const current = () => {
      const now = store.get();
      return currentRequest && now.status === "authenticated" && now.userID === userID && now.generation === shared.generation;
    };
    // Let StrictMode cleanup cancel the first effect before sending a POST.
    Promise.resolve().then(() => current() ? previewBrowserRepeat(orderID, shared.csrfToken, { expectedUserID: userID, signal: controller.signal }) : null).then((preview) => {
      if (!preview || !current()) return;
      setReview({ preview, drafts: repeatDrafts(preview, latest.current ?? undefined), key: requestKey });
      setErrors({});
      if (latest.current) { setNotice("Условия обновились. Проверьте позиции и подтвердите добавление снова."); setFocus("repeat-notice"); }
    }, async (error: unknown) => {
      if (!current()) return;
      if (error instanceof ResponseOwnerError || (error instanceof BrowserRequestError && error.code === "authenticated_user_changed")) await store.recoverOwner(shared.generation);
      else if (error instanceof BrowserRequestError && error.status === 401) store.invalidate("anonymous");
      else if (error instanceof BrowserRequestError && error.status === 403 && error.code === "csrf_failed" && !csrfRecovery.current) {
        csrfRecovery.current = true;
        await store.refresh();
      } else setFailure({ key: requestKey, status: error instanceof BrowserRequestError && error.status === 404 ? "notFound" : "failed" });
    });
    return () => { currentRequest = false; controller.abort(); };
  }, [active, failure?.key, orderID, phase, requestKey, review?.key, store, userID]);

  useEffect(() => {
    if (!active || !focus) return;
    const element = document.getElementById(focus);
    if (element) { element.focus(); setFocus(null); }
  }, [active, focus, phase, review]);

  const owned = active && review && (review.key === requestKey || phase !== "idle") ? review : null;
  const locked = phase !== "idle" || cart.mutationPending;
  const included = owned?.preview.items.filter((line) => owned.drafts[line.sourcePosition]?.include) ?? [];
  const capable = owned?.preview.items.some(canRepeat);
  const duplicates = new Set(included.map((line) => line.productId)).size < included.length;

  async function send(attempt: RepeatAttempt, checking: boolean) {
    inFlight.current = true;
    setPhase(checking ? "checking" : "submitting");
    try {
      const result = await cart.commitRepeat(attempt);
      const now = store.get();
      if (!alive.current || now.status !== "authenticated" || now.userID !== userID) return;
      if (result.kind === "added") { frozen.current = null; setPhase("added"); router.replace("/cart"); return; }
      if (result.kind === "unknown") {
        setPhase("unknown");
        setNotice("Результат добавления неизвестен. Товары могли быть добавлены. Нажмите «Проверить добавление»: проверка того же запроса не добавит их второй раз.");
        setFocus("repeat-check"); return;
      }
      if (result.kind === "blocked") {
        setPhase(checking ? "unknown" : "idle");
        if (!checking) frozen.current = null;
        setNotice("Дождитесь завершения текущего изменения корзины и повторите действие."); return;
      }
      frozen.current = null;
      setPhase("idle");
      if (result.kind === "changed" || result.kind === "outOfStock" || result.kind === "csrf") {
        setNotice(result.kind === "outOfStock" ? "Остаток изменился. Обновляем условия повтора." : "Условия изменились. Обновляем обзор перед новым подтверждением.");
        setRevision((value) => value + 1); setFocus("repeat-notice");
      } else if (result.kind === "notFound") { setPhase("stopped"); setFailure({ key: requestKey, status: "notFound" }); }
      else if (result.kind === "idempotencyReused") {
        setPhase("stopped"); setNotice("Не удалось подтвердить добавление: ключ запроса уже использован с другими данными. Вернитесь к заказам и откройте обзор заново."); setFocus("repeat-notice");
      } else if (result.kind === "invalid") {
        const next: Record<number, string> = {};
        for (const field of result.fields) {
          const match = /^items\[(\d+)\]/.exec(field.field);
          const row = match ? attempt.body.items[Number(match[1])] : undefined;
          if (row) next[row.sourcePosition] = field.message;
        }
        setErrors(next); setNotice(result.detail || "Добавление не выполнено. Проверьте количество и выбранные позиции.");
        setFocus(Object.keys(next).length ? `repeat-quantity-${Object.keys(next)[0]}` : "repeat-notice");
      } else { setReview(null); latest.current = null; setNotice(""); setRevision((value) => value + 1); }
    } finally { inFlight.current = false; }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    if (inFlight.current || frozen.current || locked || !owned || !included.length) return;
    const next = Object.fromEntries(included.map((line) => [line.sourcePosition, additionError(line, owned.drafts[line.sourcePosition].quantity)]).filter(([, error]) => error));
    setErrors(next);
    if (Object.keys(next).length) { setFocus(`repeat-quantity-${Object.keys(next)[0]}`); return; }
    const attempt = Object.freeze({ userID, orderID: owned.preview.orderId, key: crypto.randomUUID(), body: repeatBody(owned.preview, owned.drafts) });
    frozen.current = attempt;
    setNotice("");
    void send(attempt, false);
  }
  function edit(position: number, patch: Partial<RepeatDrafts[number]>) {
    if (locked || !owned) return;
    setReview({ ...owned, drafts: { ...owned.drafts, [position]: { ...owned.drafts[position], ...patch } } });
    setErrors((old) => ({ ...old, [position]: "" }));
  }
  if (failure?.status === "notFound" && (failure.key === requestKey || phase === "stopped")) return <section className={cartStyles.state}><h2>Заказ не найден</h2><p>Заказ больше недоступен в этом аккаунте.</p></section>;
  if (failure?.key === requestKey) return <section className={cartStyles.state}><h2>Не удалось загрузить условия повтора</h2><Button onClick={() => { csrfRecovery.current = false; void store.refresh(); }}>Повторить загрузку</Button></section>;
  if (!owned) return <RepeatSkeleton />;
  return <form className={styles.review} noValidate onSubmit={submit}>
    <h2>Из заказа {owned.preview.orderNumber}</h2>
    <p>Выберите позиции и количество единиц, которые нужно добавить к уже имеющимся в корзине. Цены и наличие проверяются при подтверждении; товар не резервируется.</p>
    {notice ? <p className={styles.notice} id="repeat-notice" role="alert" tabIndex={-1}>{notice}</p> : null}
    <ul className={styles.list} aria-label="Позиции для повтора">
      {owned.preview.items.map((line) => {
        const draft = owned.drafts[line.sourcePosition];
        const id = `repeat-quantity-${line.sourcePosition}`;
        const p = line.product;
        const error = errors[line.sourcePosition];
        const number = Number(draft.quantity);
        const final = /^\d+$/.test(draft.quantity) && Number.isSafeInteger(number + line.existingCartQuantity) ? number + line.existingCartQuantity : null;
        return <li className={styles.row} key={line.sourcePosition} data-source-position={line.sourcePosition}>
          <div className={styles.history}><h3>{line.name}</h3><p>Арт. {line.sku}</p><p>В заказе: {line.requestedQuantity} · Цена тогда: {formatMoney(line.originalUnitPriceMinor, "RUB")}</p></div>
          <p className={styles.status}>{statuses[line.status]}</p>
          {p ? <div><p>Сейчас: {p.name} · {formatMoney(p.priceMinor, p.currency)}</p>
            {line.priceChanged ? <p>Цена изменилась с момента заказа.</p> : null}
            <p id={`${id}-hint`}>Уже в корзине: {line.existingCartQuantity}. Итог: минимум {p.minimumQuantity}, шаг {p.quantityStep}. В наличии: {p.stock}.</p>
          </div> : <p>Текущего предложения для исходного товара нет.</p>}
          <label className={styles.include}><input type="checkbox" checked={draft.include} disabled={locked || !canRepeat(line)} onChange={(event) => edit(line.sourcePosition, { include: event.target.checked })} />Включить: {line.name} (позиция {line.sourcePosition})</label>
          {canRepeat(line) ? <div className={styles.editor}>
            <label htmlFor={id}>Добавить единиц: {line.name} (позиция {line.sourcePosition})</label>
            <input id={id} type="text" inputMode="numeric" autoComplete="off" value={draft.quantity} disabled={locked || !draft.include}
              aria-describedby={`${id}-hint ${id}-arithmetic${error ? ` ${id}-error` : ""}`} aria-invalid={Boolean(error)}
              onChange={(event) => edit(line.sourcePosition, { quantity: event.target.value })} />
            <p id={`${id}-arithmetic`}>{draft.include ? `${line.existingCartQuantity} + ${draft.quantity || "…"} = ${final ?? "…"} в корзине` : "Исключено: товар не будет добавлен."}</p>
            {error ? <p className={styles.error} id={`${id}-error`} role="alert">{error}</p> : null}
          </div> : null}
          {line.replacement ? <aside className={styles.replacement} aria-label="Возможная замена">
            <h4>Возможная замена</h4><Link href={`/products/${encodeURIComponent(line.replacement.slug)}`} prefetch={false}>{line.replacement.name}</Link>
            <p>Арт. {line.replacement.sku} · {formatMoney(line.replacement.priceMinor, line.replacement.currency)}</p>
            <p>Для информации. Замена не включена в повтор и не будет добавлена.</p>
          </aside> : null}
        </li>;
      })}
    </ul>
    {duplicates ? <p>Несколько позиций относятся к одному товару. Сервер проверит их суммарное добавление, остаток, минимум и шаг.</p> : null}
    {!capable ? <p>В этом заказе нет товаров, доступных для повтора.</p> : !included.length ? <p>Включите хотя бы одну позицию для добавления.</p> : null}
    {phase === "unknown" || phase === "checking" ? <Button id="repeat-check" disabled={phase === "checking" || cart.mutationPending} onClick={() => { if (!inFlight.current && frozen.current) void send(frozen.current, true); }}>Проверить добавление</Button>
      : <Button type="submit" disabled={locked || !included.length}>Добавить выбранное в корзину</Button>}
    <p role="status" aria-live="polite">{phase === "submitting" ? "Добавляем товары…" : phase === "checking" ? "Проверяем результат добавления…" : phase === "added" ? "Товары добавлены. Открываем корзину…" : ""}</p>
  </form>;
}
