"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { BrowserRequestError, ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Order } from "@/lib/api/order";
import { getBrowserOrders } from "@/lib/api/order-client";
import { formatMoney } from "@/lib/money";
import { pluralRu } from "@/lib/plural";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import cartStyles from "./cart.module.css";
import styles from "./orders.module.css";

type Loaded = { owner: string; generation: number } & (
  | { status: "ready"; orders: Order[] }
  | { status: "failed" }
);

function OrderSummary({ order }: { order: Order }) {
  // Order has no currency field. Match detail's documented RUB assumption.
  const total = formatMoney(order.totalMinor, "RUB");
  const date = new Intl.DateTimeFormat("ru-RU", { dateStyle: "long" }).format(new Date(order.createdAt));
  return <article className={styles.order}>
    <div className={styles.identity}>
      <h2>Заказ {order.number}</h2>
      <time dateTime={order.createdAt}>{date}</time>
      <p>{order.statusLabel || order.status}</p>
    </div>
    <dl className={styles.facts}>
      <div><dt>Количество</dt><dd>{order.itemCount} {pluralRu(order.itemCount, ["единица", "единицы", "единиц"])}</dd></div>
      <div><dt>Сумма заказа</dt><dd className={styles.total}>{total}</dd></div>
      {order.deliveryAddress ? <div className={styles.address}><dt>Адрес доставки</dt><dd>{order.deliveryAddress}</dd></div> : null}
    </dl>
    <Link className={styles.detail} href={`/account/orders/${encodeURIComponent(order.id)}`} prefetch={false}
      aria-label={`Подробнее о заказе ${order.number}`}>Подробнее о заказе</Link>
  </article>;
}

export function OrdersView({ initialSession, loginHref }: { initialSession: RouteSession; loginHref: string | null }) {
  const store = useBrowserSessionStore();
  const session = useBrowserSession();
  const routeKey = initialSession.status === "authenticated" ? `user:${initialSession.userID}` : initialSession.status;
  const [enteredKey, setEnteredKey] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);

  // This route uses only the shared session; it never initializes Cart/Favorites.
  useEffect(() => {
    void store.enterRoute(initialSession);
    setEnteredKey(routeKey);
  }, [store, initialSession, routeKey]);

  const entered = enteredKey === routeKey;
  const userID = entered && session.status === "authenticated" ? session.userID : null;
  const generation = session.generation;
  useEffect(() => {
    if (!userID) return;
    const controller = new AbortController();
    let active = true;
    const current = () => {
      const now = store.get();
      return active && now.status === "authenticated" && now.userID === userID && now.generation === generation;
    };
    getBrowserOrders({ expectedUserID: userID, signal: controller.signal }).then((orders) => {
      if (current()) setLoaded({ status: "ready", orders, owner: userID, generation });
    }, async (error: unknown) => {
      if (!current()) return;
      if (error instanceof ResponseOwnerError || (error instanceof BrowserRequestError && error.code === "authenticated_user_changed")) {
        await store.recoverOwner(generation);
      } else if (error instanceof BrowserRequestError && error.status === 401) store.invalidate("anonymous");
      else setLoaded({ status: "failed", owner: userID, generation });
    });
    return () => { active = false; controller.abort(); };
  }, [generation, store, userID]);

  // Hide old private content during every identity check, including A → A.
  const owned = loaded && userID !== null && loaded.owner === userID && loaded.generation === generation ? loaded : null;
  const status = !entered ? (initialSession.status === "authenticated" ? "loading" : initialSession.status)
    : session.status === "anonymous" || session.status === "unavailable" ? session.status
      : owned?.status ?? "loading";
  let content;
  if (status === "anonymous") {
    content = <section className={cartStyles.state}>
      <h2>Войдите, чтобы увидеть заказы</h2>
      <p>История покупок доступна в вашем аккаунте.</p>
      <div className={cartStyles.actions}>
        {loginHref ? <a href={loginHref}>Войти</a> : null}
        <Button onClick={() => void store.refresh()} variant="secondary">Проверить вход</Button>
      </div>
    </section>;
  } else if (status === "unavailable" || status === "failed") {
    content = <section className={cartStyles.state}>
      <h2>История заказов временно недоступна</h2>
      <p>Не удалось проверить вход или загрузить заказы.</p>
      <Button onClick={() => void store.refresh()} variant="secondary">Повторить загрузку</Button>
    </section>;
  } else if (owned?.status === "ready") {
    content = owned.orders.length ? <ul className={styles.list} aria-label="История заказов">
      {owned.orders.map((order) => <li key={order.id}><OrderSummary order={order} /></li>)}
    </ul> : <section className={cartStyles.state}>
      <h2>Заказов пока нет</h2>
      <p>После первой покупки здесь появится история заказов.</p>
      <Link className={cartStyles.secondaryLink} href="/catalog">Перейти в каталог</Link>
    </section>;
  } else content = <OrdersSkeleton />;

  return <main className={cartStyles.main} id="main-content"><Container>
    <header className={cartStyles.heading}><h1>Мои заказы</h1></header>
    {content}
  </Container></main>;
}

export function OrdersSkeleton() {
  return <div className={styles.list} aria-busy="true" aria-label="Загружаем историю заказов">
    <div className={styles.skeleton} aria-hidden="true" />
    <div className={styles.skeleton} aria-hidden="true" />
  </div>;
}
