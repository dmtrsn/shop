"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Cart, CartSummary } from "@/lib/api/cart";
import { checkoutReview } from "@/lib/api/order-client";
import { formatMoney } from "@/lib/money";
import { pluralRu } from "@/lib/plural";

import { useBrowserSession } from "./browser-session-provider";
import { useCart, type PlaceOrderAttempt, type PlaceOrderResult } from "./cart-provider";
import cartStyles from "./cart.module.css";
import styles from "./checkout.module.css";

const positions = (count: number) => `${count} ${pluralRu(count, ["позиция", "позиции", "позиций"])}`;
const units = (count: number) => `${count} ${pluralRu(count, ["единица", "единицы", "единиц"])}`;

// A read-only copy of Go values for one review. Displayed and compared only.
type ReviewLine = {
  productId: string; name: string; sku: string; manufacturer: string; currency: string;
  quantity: number; unitPriceMinor: number; lineTotalMinor: number; issue?: string;
};
type Review = { lines: ReviewLine[]; summary: CartSummary; currency: string; unselected: number };
type Frozen = { attempt: PlaceOrderAttempt; review: Review };
type Phase = "idle" | "submitting" | "ambiguous" | "checking" | "placed";
type Notice = { tone: "alert" | "status"; title: string; text: string; cartLink?: boolean };
type Fields = Partial<Record<"deliveryAddress" | "comment", string>>;
type Focus = "notice" | "deliveryAddress" | "comment" | "check" | null;

function reviewOf(cart: Cart): Review {
  const lines = cart.items.filter((line) => line.selected).map((line): ReviewLine => ({
    productId: line.product.id, name: line.product.name, sku: line.product.sku,
    manufacturer: line.product.manufacturer.name, currency: line.product.currency, quantity: line.quantity,
    unitPriceMinor: line.product.priceMinor, lineTotalMinor: line.lineTotalMinor,
    ...(line.issue ? { issue: line.issue } : {}),
  }));
  return {
    lines, summary: cart.summary, currency: cart.items[0]?.product.currency ?? "RUB",
    unselected: cart.items.length - lines.length,
  };
}

// Compares the review the shopper confirmed with the refreshed Go review.
function reviewChanges(previous: Review, next: Review) {
  const notes = new Map<string, string[]>();
  const before = new Map(previous.lines.map((line) => [line.productId, line]));
  for (const line of next.lines) {
    const old = before.get(line.productId);
    const lineNotes: string[] = [];
    if (!old) lineNotes.push("Новая позиция в заказе");
    else {
      if (old.quantity !== line.quantity) lineNotes.push(`Количество изменилось: было ${old.quantity}, стало ${line.quantity}`);
      if (old.unitPriceMinor !== line.unitPriceMinor) {
        lineNotes.push(`Цена изменилась: было ${formatMoney(old.unitPriceMinor, old.currency)}, стало ${formatMoney(line.unitPriceMinor, line.currency)}`);
      }
    }
    if (lineNotes.length) notes.set(line.productId, lineNotes);
  }
  const current = new Set(next.lines.map((line) => line.productId));
  const removed = previous.lines.filter((line) => !current.has(line.productId));
  const total = previous.summary.totalMinor !== next.summary.totalMinor
    ? `Итого изменилось: было ${formatMoney(previous.summary.totalMinor, previous.currency)}, стало ${formatMoney(next.summary.totalMinor, next.currency)}.`
    : "";
  return { notes, removed, total };
}

function definitiveNotice(result: PlaceOrderResult): Notice | null {
  switch (result.kind) {
    case "changed":
      return { tone: "alert", title: "Заказ не оформлен: корзина изменилась", text: "Цены, количество или состав выбранных товаров изменились. Проверьте обновлённый заказ и подтвердите его снова." };
    case "outOfStock":
      return { tone: "alert", title: "Заказ не оформлен: товара недостаточно", text: "Остаток одного из выбранных товаров изменился. Исправьте позиции в корзине и подтвердите заказ снова.", cartLink: true };
    case "conflict":
      return { tone: "alert", title: "Заказ не оформлен", text: "В корзине нет выбранных товаров или она изменилась. Проверьте корзину.", cartLink: true };
    case "csrf":
      return { tone: "alert", title: "Заказ не отправлен", text: "Мы проверили вход и обновили корзину. Проверьте заказ и подтвердите его снова." };
    case "idempotencyReused":
      return { tone: "alert", title: "Не удалось подтвердить заказ", text: "Этот запрос уже использовался для другого заказа. Ничего не изменено. Вернитесь в корзину и проверьте её перед новым оформлением.", cartLink: true };
    case "ownerMismatch":
    case "discarded":
      return { tone: "alert", title: "Сессия изменилась", text: "Заказ прежней сессии не отправлен повторно. Проверьте вход и корзину." };
    case "invalid":
      return result.fields.some((field) => field.field === "deliveryAddress" || field.field === "comment") ? null
        : { tone: "alert", title: "Заказ не оформлен", text: result.detail ?? "Проверьте заполненные поля." };
    default:
      return null;
  }
}

function ReviewList({ review, notes }: { review: Review; notes?: Map<string, string[]> }) {
  return (
    <ul className={styles.lines}>
      {review.lines.map((line) => (
        <li className={styles.line} key={line.productId}>
          <div className={styles.lineIdentity}>
            <p className={styles.lineName}>{line.name}</p>
            <p className={cartStyles.meta}>Арт. {line.sku} · {line.manufacturer}</p>
            {line.issue ? <p className={cartStyles.issue}>Проблема наличия: {line.issue}</p> : null}
            {notes?.get(line.productId)?.map((note) => <p className={styles.change} key={note}>Изменение: {note}</p>)}
          </div>
          <dl className={styles.lineFigures}>
            <div><dt>Количество</dt><dd>{line.quantity}</dd></div>
            <div><dt>Цена</dt><dd>{formatMoney(line.unitPriceMinor, line.currency)}</dd></div>
            <div><dt>Сумма</dt><dd>{formatMoney(line.lineTotalMinor, line.currency)}</dd></div>
          </dl>
        </li>
      ))}
    </ul>
  );
}

function Summary({ review, children }: { review: Review; children?: ReactNode }) {
  const { summary, currency } = review;
  return (
    <aside aria-labelledby="checkout-summary-title" className={`${cartStyles.summary} ${styles.summary}`}>
      <h2 id="checkout-summary-title">Итоги заказа</h2>
      <p className={cartStyles.summaryScope}>{positions(review.lines.length)} · {units(summary.selectedCount)}</p>
      <dl>
        <div><dt>Товары без скидки</dt><dd>{formatMoney(summary.subtotalMinor, currency)}</dd></div>
        <div><dt>Скидка</dt><dd>{summary.discountMinor ? "−" : ""}{formatMoney(summary.discountMinor, currency)}</dd></div>
        <div><dt>Доставка</dt><dd>{formatMoney(summary.deliveryMinor, currency)}</dd></div>
        <div className={cartStyles.total}><dt>Итого</dt><dd>{formatMoney(summary.totalMinor, currency)}</dd></div>
      </dl>
      {children}
      {review.unselected ? (
        <p className={cartStyles.note}>Не выбрано и останется в корзине: {positions(review.unselected)}.</p>
      ) : null}
      <p className={cartStyles.note}>Корзина не резервирует товар: цены и остаток проверяются при подтверждении.</p>
    </aside>
  );
}

export function CheckoutView({ initialSession }: { initialSession: RouteSession }) {
  const cart = useCart();
  const session = useBrowserSession();
  const router = useRouter();
  const { enterRoute } = cart;
  const routeKey = initialSession.status === "authenticated" ? `user:${initialSession.userID}` : initialSession.status;
  const [enteredKey, setEnteredKey] = useState<string | null>(null);
  const [address, setAddress] = useState("");
  const [comment, setComment] = useState("");
  const [formOwner, setFormOwner] = useState<string | null>(null);
  const [phase, setPhase] = useState<Phase>("idle");
  const [frozen, setFrozen] = useState<Frozen | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Fields>({});
  const [previous, setPrevious] = useState<Review | null>(null);
  const [focus, setFocus] = useState<Focus>(null);
  // Synchronous guard: Enter, a second click or a fast double submit cannot
  // start another request before React re-renders the pending state.
  const inFlight = useRef(false);

  useEffect(() => {
    enterRoute(initialSession);
    setEnteredKey(routeKey);
  }, [enterRoute, initialSession, routeKey]);

  // The form and any attempt belong to one user. A different user, sign-out or
  // an unavailable session drops them; a revalidation of the same user keeps them.
  const identity = session.status === "authenticated" ? session.userID
    : session.status === "anonymous" || session.status === "unavailable" ? null : undefined;
  useEffect(() => {
    if (identity === undefined || identity === formOwner) return;
    if (formOwner !== null) {
      setAddress(""); setComment(""); setFrozen(null); setPhase("idle");
      setFieldErrors({}); setPrevious(null); setNotice(null);
    }
    setFormOwner(identity);
  }, [identity, formOwner]);

  useEffect(() => {
    if (!focus) return;
    const id = { notice: "checkout-notice", deliveryAddress: "checkout-address", comment: "checkout-comment", check: "checkout-check" }[focus];
    document.getElementById(id)?.focus();
    setFocus(null);
  }, [focus]);

  const entered = enteredKey === routeKey;
  const status = entered ? cart.status : initialSession.status === "authenticated" ? "loading" : initialSession.status;
  const snapshot = entered && status === "ready" ? cart.cart : null;
  const live = snapshot ? reviewOf(snapshot) : null;
  const locked = phase !== "idle";
  // While an attempt is open, the shopper sees exactly what was sent.
  const review = locked && frozen ? frozen.review : live;
  const issues = live?.lines.filter((line) => line.issue) ?? [];
  const busy = cart.mutationPending || cart.reconciling;
  const ready = !locked && live !== null && live.lines.length > 0 && !issues.length && !busy;
  const changes = !locked && previous && live ? reviewChanges(previous, live) : null;

  function settle(attempt: Frozen, result: PlaceOrderResult, checking: boolean) {
    if (result.kind === "placed") {
      setPhase("placed");
      router.replace(`/account/orders/${encodeURIComponent(result.order.id)}`);
      return;
    }
    if (result.kind === "unknown") {
      // Keep the frozen key and body; never declare success or failure.
      setPhase("ambiguous");
      setNotice({
        tone: "alert", title: checking ? "Результат всё ещё неизвестен" : "Не удалось узнать результат оформления",
        text: "Заказ мог быть создан. Не оформляйте его заново: нажмите «Проверить заказ» — повторная проверка не создаст второй заказ.",
      });
      setFocus("check");
      return;
    }
    if (result.kind === "blocked") {
      setPhase(checking ? "ambiguous" : "idle");
      if (!checking) setFrozen(null);
      setNotice({ tone: "status", title: "Корзина обновляется", text: "Дождитесь обновления и повторите действие." });
      return;
    }
    // Definitive: no order was created by this attempt. The next submit is a
    // new logical attempt with a new key.
    setFrozen(null);
    setPhase("idle");
    if (result.kind === "changed" || result.kind === "outOfStock" || result.kind === "conflict") setPrevious(attempt.review);
    if (result.kind === "invalid") {
      const fields: Fields = {};
      for (const field of result.fields) {
        if ((field.field === "deliveryAddress" || field.field === "comment") && !fields[field.field]) fields[field.field] = field.message;
      }
      setFieldErrors(fields);
      if (fields.deliveryAddress || fields.comment) setFocus(fields.deliveryAddress ? "deliveryAddress" : "comment");
    }
    const next = definitiveNotice(result);
    setNotice(next);
    if (next) setFocus("notice");
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || !ready || !snapshot || !live || session.status !== "authenticated") return;
    if (!address.trim()) {
      setFieldErrors({ deliveryAddress: "Укажите адрес доставки" });
      setFocus("deliveryAddress");
      return;
    }
    inFlight.current = true;
    const attempt: Frozen = {
      attempt: {
        userID: session.userID,
        key: crypto.randomUUID(),
        body: { deliveryAddress: address, ...(comment ? { comment } : {}), ...checkoutReview(snapshot) },
      },
      review: live,
    };
    setFrozen(attempt); setPhase("submitting"); setNotice(null); setFieldErrors({}); setPrevious(null);
    try {
      settle(attempt, await cart.placeOrder(attempt.attempt, "submit"), false);
    } finally {
      inFlight.current = false;
    }
  }

  // Deliberate check after an unknown outcome: the same key and exact body.
  async function check() {
    if (inFlight.current || phase !== "ambiguous" || !frozen) return;
    inFlight.current = true;
    setPhase("checking");
    try {
      settle(frozen, await cart.placeOrder(frozen.attempt, "check"), true);
    } finally {
      inFlight.current = false;
    }
  }

  function edit(field: "deliveryAddress" | "comment", value: string) {
    if (locked) return;
    (field === "deliveryAddress" ? setAddress : setComment)(value);
    if (fieldErrors[field]) setFieldErrors({ ...fieldErrors, [field]: undefined });
  }

  const describe = (...ids: Array<string | false | undefined>) => ids.filter(Boolean).join(" ") || undefined;
  let content: ReactNode;
  if (phase === "placed") {
    content = <p className={styles.progress} role="status">Заказ оформлен. Открываем подтверждение…</p>;
  } else if (status === "anonymous" && !locked) {
    content = (
      <section className={cartStyles.state}>
        <h2>Войдите, чтобы оформить заказ</h2>
        <p>Оформление доступно в вашем аккаунте. После входа вернитесь в корзину.</p>
        <div className={cartStyles.actions}>
          {cart.loginHref ? <a href={cart.loginHref}>Войти</a> : null}
          <Button onClick={() => void cart.refresh()} variant="secondary">Проверить вход</Button>
          <Link href="/cart">Вернуться в корзину</Link>
        </div>
      </section>
    );
  } else if (status === "unavailable" && !locked) {
    content = (
      <section className={cartStyles.state}>
        <h2>Оформление временно недоступно</h2>
        <p>Не удалось проверить вход или загрузить корзину. Заказ можно подтвердить только после успешной загрузки.</p>
        <Button onClick={() => void cart.refresh()} variant="secondary">Повторить загрузку</Button>
      </section>
    );
  } else if (!review) {
    content = <div aria-busy="true" aria-label="Загружаем заказ" className={cartStyles.layout}>
      <div aria-hidden="true" className={cartStyles.skeletonRow} />
      <div aria-hidden="true" className={cartStyles.skeletonSummary} />
    </div>;
  } else if (!locked && !review.lines.length) {
    content = (
      <section className={cartStyles.state}>
        <h2>Нет товаров, выбранных для заказа</h2>
        <p>Отметьте товары в корзине, чтобы оформить их. Если вы только что оформили заказ, выбранные позиции уже убраны из корзины.</p>
        <div className={cartStyles.actions}><Link href="/cart">Вернуться в корзину</Link></div>
      </section>
    );
  } else {
    const ambiguous = phase === "ambiguous" || phase === "checking";
    content = (
      <div className={styles.layout}>
        <section aria-labelledby="checkout-review-title" className={styles.review}>
          <h2 id="checkout-review-title">Товары в заказе</h2>
          {issues.length && !locked ? (
            <div className={styles.blocking}>
              <p><strong>Оформить заказ пока нельзя.</strong> Для выбранных позиций есть проблема наличия:</p>
              <ul>{issues.map((line) => <li key={line.productId}>{line.name}: {line.issue}</li>)}</ul>
              <Link href="/cart">Исправить в корзине</Link>
            </div>
          ) : null}
          {changes?.removed.length ? (
            <div className={styles.changes}>
              <p>Больше не входит в заказ:</p>
              <ul>{changes.removed.map((line) => <li key={line.productId}>{line.name} · {units(line.quantity)}</li>)}</ul>
            </div>
          ) : null}
          {changes?.total ? <p className={styles.change}>Изменение: {changes.total}</p> : null}
          <ReviewList notes={changes?.notes} review={review} />
        </section>
        <Summary review={review} />
        <form aria-labelledby="checkout-delivery-title" className={styles.form} noValidate onSubmit={(event) => void submit(event)}>
          <h2 id="checkout-delivery-title">Доставка</h2>
          <div className={styles.field}>
            <label htmlFor="checkout-address">Адрес доставки</label>
            <p className={styles.hint} id="checkout-address-hint">Город, улица, дом, квартира или офис.</p>
            <textarea
              aria-describedby={describe("checkout-address-hint", fieldErrors.deliveryAddress && "checkout-address-error")}
              aria-invalid={fieldErrors.deliveryAddress ? true : undefined}
              aria-required="true"
              autoComplete="street-address"
              id="checkout-address"
              name="deliveryAddress"
              onChange={(event) => edit("deliveryAddress", event.target.value)}
              readOnly={locked}
              rows={3}
              value={locked && frozen ? frozen.attempt.body.deliveryAddress : address}
            />
            {fieldErrors.deliveryAddress ? <p className={cartStyles.error} id="checkout-address-error">{fieldErrors.deliveryAddress}</p> : null}
          </div>
          <div className={styles.field}>
            <label htmlFor="checkout-comment">Комментарий к заказу <span className={styles.optional}>(необязательно)</span></label>
            <textarea
              aria-describedby={describe(fieldErrors.comment && "checkout-comment-error")}
              aria-invalid={fieldErrors.comment ? true : undefined}
              id="checkout-comment"
              name="comment"
              onChange={(event) => edit("comment", event.target.value)}
              readOnly={locked}
              rows={2}
              value={locked && frozen ? frozen.attempt.body.comment ?? "" : comment}
            />
            {fieldErrors.comment ? <p className={cartStyles.error} id="checkout-comment-error">{fieldErrors.comment}</p> : null}
          </div>
          {ambiguous ? (
            <div className={styles.actions}>
              <Button
                aria-describedby="checkout-notice"
                aria-disabled={phase === "checking"}
                id="checkout-check"
                onClick={() => void check()}
              >
                {phase === "checking" ? "Проверяем заказ…" : "Проверить заказ"}
              </Button>
              <Link href="/cart">Вернуться в корзину</Link>
              <p className={cartStyles.note}>Если уйти, результат оформления останется неизвестным, пока вы не проверите заказ.</p>
            </div>
          ) : (
            <div className={styles.actions}>
              <Button aria-describedby="checkout-submit-note" aria-disabled={!ready} type="submit">
                {phase === "submitting" ? "Оформляем заказ…" : "Подтвердить заказ"}
              </Button>
              <p className={cartStyles.note} id="checkout-submit-note">
                {busy && !locked ? "Корзина обновляется. Подтвердить заказ можно после обновления."
                  : `Будет оформлено: ${positions(review.lines.length)} на сумму ${formatMoney(review.summary.totalMinor, review.currency)}.`}
              </p>
            </div>
          )}
        </form>
      </div>
    );
  }

  return (
    <main className={cartStyles.main} id="main-content">
      <Container>
        <header className={cartStyles.heading}><h1>Оформление заказа</h1></header>
        {notice && phase !== "placed" ? (
          <div
            className={notice.tone === "alert" ? styles.alert : styles.notice}
            id="checkout-notice"
            role={notice.tone === "alert" ? "alert" : "status"}
            tabIndex={-1}
          >
            <p className={styles.alertTitle}>{notice.title}</p>
            <p>{notice.text}</p>
            {notice.cartLink ? <Link href="/cart">Вернуться в корзину</Link> : null}
          </div>
        ) : null}
        <p aria-live="polite" className="visually-hidden" role="status">
          {phase === "submitting" ? "Отправляем заказ…" : phase === "checking" ? "Проверяем заказ…" : ""}
        </p>
        {content}
      </Container>
    </main>
  );
}
