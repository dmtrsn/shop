"use client";

import Link from "next/link";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import type { Cart } from "@/lib/api/cart";
import { formatMoney } from "@/lib/money";
import { pluralRu } from "@/lib/plural";

import { selectionKey, useCart, type CartRouteSession } from "./cart-provider";
import { CartRow } from "./cart-row";
import styles from "./cart.module.css";

const positions = (count: number) => `${count} ${pluralRu(count, ["позиция", "позиции", "позиций"])}`;
const units = (count: number) => `${count} ${pluralRu(count, ["единица", "единицы", "единиц"])}`;
// Genitive after «из»: «из 1 позиции», «из 3 позиций».
const ofPositions = (count: number) => `${count} ${pluralRu(count, ["позиции", "позиций", "позиций"])}`;

export function CartSkeleton() {
  return (
    <div aria-busy="true" aria-label="Загружаем корзину" className={styles.layout}>
      <div className={styles.list}>
        {[0, 1, 2].map((index) => <div aria-hidden="true" className={styles.skeletonRow} key={index} />)}
      </div>
      <div aria-hidden="true" className={styles.skeletonSummary} />
    </div>
  );
}

function matches(item: Cart["items"][number], query: string) {
  const { product } = item;
  return `${product.name} ${product.sku} ${product.manufacturer.name}`.toLocaleLowerCase("ru").includes(query);
}

// Checkout is only a link: it never writes. It is offered for a current,
// settled Cart with at least one selected line and no selected issue, because
// Go aborts checkout when any selected line lacks stock.
function CheckoutLink({ cart, busy }: { cart: Cart; busy: boolean }) {
  const selected = cart.items.filter((line) => line.selected);
  if (!selected.length) return <p className={styles.note}>Выберите товары, чтобы оформить заказ.</p>;
  if (selected.some((line) => line.issue)) {
    return <p className={styles.issue}>Чтобы оформить заказ, снимите выбор с позиций с проблемой наличия или исправьте их количество.</p>;
  }
  return (
    <Link
      aria-describedby={busy ? "cart-checkout-busy" : undefined}
      aria-disabled={busy || undefined}
      className={styles.checkout}
      href="/checkout"
      onClick={(event) => { if (busy) event.preventDefault(); }}
    >
      Перейти к оформлению
      {busy ? <span className="visually-hidden" id="cart-checkout-busy"> (корзина обновляется)</span> : null}
    </Link>
  );
}

function CartSummaryPanel({ cart, reconciling, busy }: { cart: Cart; reconciling: boolean; busy: boolean }) {
  const currency = cart.items[0]?.product.currency ?? "RUB";
  const selectedPositions = cart.items.filter((line) => line.selected).length;
  const { summary } = cart;
  // Display only: a nonzero Go discount is shown with a minus sign.
  const rows: Array<[string, string]> = [
    ["Товары без скидки", formatMoney(summary.subtotalMinor, currency)],
    ["Скидка", `${summary.discountMinor ? "−" : ""}${formatMoney(summary.discountMinor, currency)}`],
    ["Доставка", formatMoney(summary.deliveryMinor, currency)],
  ];
  return (
    <aside aria-busy={reconciling} aria-labelledby="cart-summary-title" className={styles.summary} id="cart-summary">
      <h2 id="cart-summary-title">Итоги выбранного</h2>
      <p className={styles.summaryScope}>
        Выбрано {selectedPositions} из {ofPositions(summary.positionCount)} · {units(summary.selectedCount)}
      </p>
      {reconciling ? <p className={styles.stale}>Сверяем корзину с сервером. Итоги могут быть неактуальны.</p> : null}
      <dl>
        {rows.map(([label, amount]) => (
          <div key={label}><dt>{label}</dt><dd>{amount}</dd></div>
        ))}
        <div className={styles.total}><dt>Итого</dt><dd>{formatMoney(summary.totalMinor, currency)}</dd></div>
      </dl>
      {summary.unavailableCount ? (
        <p className={styles.issue}>
          С проблемой наличия: {positions(summary.unavailableCount)}. Они не входят в итоги.
        </p>
      ) : null}
      <CheckoutLink busy={busy} cart={cart} />
      <p className={styles.note}>Корзина не резервирует товар.</p>
      <Link className={styles.secondaryLink} href="/catalog">Продолжить покупки</Link>
    </aside>
  );
}

export function CartView({ initialSession }: { initialSession: CartRouteSession }) {
  const state = useCart();
  const { enterRoute } = state;
  const routeKey = initialSession.status === "authenticated" ? `user:${initialSession.userID}` : initialSession.status;
  const [enteredKey, setEnteredKey] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const root = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const focusedProduct = useRef<string | null>(null);
  const previousIDs = useRef<string[]>([]);
  useEffect(() => {
    enterRoute(initialSession);
    setEnteredKey(routeKey);
  }, [enterRoute, initialSession, routeKey]);

  // Before the route effect, render only its sanitized server gate. A warm
  // provider from another page must never flash its previous private Cart.
  const entered = enteredKey === routeKey;
  const status = entered ? state.status : initialSession.status === "authenticated" ? "loading" : initialSession.status;
  const cart = entered && status === "ready" ? state.cart : null;

  // After a confirmed removal, move focus to the next visible row's Remove
  // control, then the previous one, then the page heading.
  useLayoutEffect(() => {
    if (!cart) return;
    const ids = cart.items.map((line) => line.product.id);
    const focused = focusedProduct.current;
    if (focused && !ids.includes(focused) && document.activeElement === document.body) {
      const index = previousIDs.current.indexOf(focused);
      const candidates = [...previousIDs.current.slice(index + 1), ...previousIDs.current.slice(0, index).reverse()];
      const controls = Array.from(root.current?.querySelectorAll<HTMLElement>("[data-cart-remove]") ?? [])
        .filter((control) => !control.closest("[hidden]"));
      const target = candidates.map((id) => controls.find((control) => control.dataset.cartRemove === id)).find(Boolean);
      focusedProduct.current = null;
      (target ?? heading.current)?.focus();
    }
    previousIDs.current = ids;
  }, [cart]);

  const busy = state.mutationPending || state.reconciling;
  const normalized = query.trim().toLocaleLowerCase("ru");
  const visible = new Set(cart?.items.filter((line) => matches(line, normalized)).map((line) => line.product.id));
  function toggleGroup(id: string) {
    setCollapsed((previous) => {
      const next = new Set(previous);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  return (
    <main className={styles.main} id="main-content">
      <Container>
        <div ref={root} onFocusCapture={(event) => {
          const row = (event.target as HTMLElement).closest<HTMLElement>("[data-cart-row]");
          focusedProduct.current = row?.dataset.cartRow ?? null;
        }}>
          <header className={styles.heading}>
            <h1 ref={heading} tabIndex={-1}>Корзина</h1>
            {cart?.items.length ? (
              <p>{positions(cart.summary.positionCount)} · {units(cart.summary.itemCount)}</p>
            ) : null}
          </header>
          {status === "anonymous" ? (
            <section className={styles.state}>
              <h2>Войдите, чтобы открыть корзину</h2>
              <p>Корзина доступна в вашем аккаунте. После входа вернитесь на эту страницу.</p>
              <div className={styles.actions}>
                {state.loginHref ? <a href={state.loginHref}>Войти</a> : null}
                <Button onClick={() => void state.refresh()} variant="secondary">Проверить вход</Button>
                <Link href="/catalog">Перейти в каталог</Link>
              </div>
            </section>
          ) : status === "unavailable" ? (
            <section className={styles.state}>
              <h2>Корзина временно недоступна</h2>
              <p>Не удалось проверить вход или загрузить корзину. Изменения недоступны до успешной загрузки.</p>
              <Button onClick={() => void state.refresh()} variant="secondary">Повторить загрузку</Button>
            </section>
          ) : cart && cart.items.length ? (
            <>
              <div className={styles.toolbar}>
                <label className={styles.search}>
                  <span>Найти в корзине</span>
                  <input
                    autoComplete="off"
                    maxLength={200}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder="Название, артикул или производитель"
                    type="search"
                    value={query}
                  />
                </label>
                <div className={styles.bulk} role="group" aria-label="Выбор во всей корзине">
                  <Button
                    aria-disabled={busy}
                    onClick={() => { if (!busy) void state.selectAll(true); }}
                    variant="secondary"
                  >
                    Выбрать всю корзину ({positions(cart.items.length)})
                  </Button>
                  <Button
                    aria-disabled={busy}
                    onClick={() => { if (!busy) void state.selectAll(false); }}
                    variant="secondary"
                  >
                    Снять выбор со всей корзины
                  </Button>
                </div>
              </div>
              {state.errors[selectionKey] ? <p className={styles.error}>{state.errors[selectionKey]}</p> : null}
              {normalized ? (
                <p className={styles.scope}>
                  Показано {visible.size} из {ofPositions(cart.items.length)}. Выбор и итоги относятся ко всей корзине.
                </p>
              ) : null}
              <a className={styles.summaryLink} href="#cart-summary">К итогам корзины</a>
              <div className={styles.layout}>
                <section aria-label="Товары в корзине" className={styles.list}>
                  {!visible.size ? <p className={styles.scope}>В корзине такого нет. Измените запрос.</p> : null}
                  {cart.groups.map((group, index) => {
                    const lines = group.items.filter((line) => visible.has(line.product.id));
                    if (!lines.length) return null;
                    const id = group.manufacturer.id;
                    const closed = collapsed.has(id);
                    const panel = `cart-group-${index}`;
                    return (
                      <section className={styles.group} key={id}>
                        <h2>
                          <button
                            aria-controls={panel}
                            aria-expanded={!closed}
                            className={styles.groupTitle}
                            onClick={() => toggleGroup(id)}
                            type="button"
                          >
                            <span>{group.manufacturer.name}</span>
                            <span className={styles.groupMeta}>
                              {positions(lines.length)}
                              {group.subtotalMinor ? ` · выбрано на ${formatMoney(group.subtotalMinor, lines[0].product.currency)}` : ""}
                            </span>
                            <span aria-hidden="true" className={styles.chevron}>{closed ? "+" : "−"}</span>
                          </button>
                        </h2>
                        <div hidden={closed} id={panel}>
                          {lines.map((item) => <CartRow item={item} key={item.product.id} />)}
                        </div>
                      </section>
                    );
                  })}
                </section>
                <CartSummaryPanel busy={busy} cart={cart} reconciling={state.reconciling} />
              </div>
            </>
          ) : cart ? (
            <section className={styles.state}>
              <h2>В корзине пока нет товаров</h2>
              <p>Добавляйте товары из каталога, подборок или избранного.</p>
              <div className={styles.actions}><Link href="/catalog">Перейти в каталог</Link></div>
            </section>
          ) : <CartSkeleton />}
        </div>
      </Container>
    </main>
  );
}
