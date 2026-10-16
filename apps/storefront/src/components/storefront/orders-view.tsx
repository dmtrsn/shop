"use client";

import { LoginLink } from "./login-link";

import { useEffect, useState } from "react";

import { Button, ButtonLink, buttonClassName } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { EmptyState } from "@/components/ui/empty-state";
import { ReceiptIcon, RepeatIcon } from "@/components/ui/icons";
import { PageHeader } from "@/components/ui/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import { UnavailableState } from "@/components/ui/unavailable-state";
import { BrowserRequestError, ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Order } from "@/lib/api/order";
import { getBrowserOrders } from "@/lib/api/order-client";
import { formatMoney } from "@/lib/money";
import { orderStatusTone } from "@/lib/order-status";
import { pluralRu } from "@/lib/plural";

import { AccountNav } from "./account-nav";
import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import accountStyles from "./account.module.css";
import cartStyles from "./cart.module.css";
import { OrderThumb } from "./order-thumb";
import styles from "./orders.module.css";

// The card previews at most this many snapshot thumbnails; the rest is a count.
const thumbLimit = 4;

type Loaded = { owner: string; generation: number } & (
  | { status: "ready"; orders: Order[] }
  | { status: "failed" }
);

export function OrderStatus({ order }: { order: Order }) {
  // Go's label is the status text; tone is a supplementary cue, never the only one.
  return <p className={`${styles.status} ${orderStatusTone(order.status) === "success" ? styles.statusSuccess : ""}`}>
    <span>{order.statusLabel || order.status}</span>
  </p>;
}

function OrderSummary({ order }: { order: Order }) {
  // Order has no currency field. Match detail's documented RUB assumption.
  const total = formatMoney(order.totalMinor, "RUB");
  const date = new Intl.DateTimeFormat("ru-RU", { dateStyle: "long" }).format(new Date(order.createdAt));
  const href = `/account/orders/${encodeURIComponent(order.id)}`;
  const more = order.items.length - thumbLimit;
  return <article className={styles.order}>
    <div className={styles.head}>
      <div className={styles.identity}>
        <h2>Заказ {order.number}</h2>
        <time dateTime={order.createdAt}>{date}</time>
      </div>
      <OrderStatus order={order} />
      <dl className={styles.total}><dt>Сумма заказа</dt><dd>{total}</dd></dl>
    </div>
    <div className={styles.body}>
      {/* Snapshot images from this order only; no per-product requests. */}
      {order.items.length ? <div className={styles.thumbs}>
        {order.items.slice(0, thumbLimit).map((item, index) => <OrderThumb image={item.image} key={`${item.productId}-${index}`} />)}
        {more > 0 ? <span aria-hidden="true" className={styles.more}>+{more}</span> : null}
      </div> : null}
      <dl className={styles.facts}>
        <div><dt>Количество</dt><dd>{order.itemCount} {pluralRu(order.itemCount, ["единица", "единицы", "единиц"])}</dd></div>
        {order.deliveryAddress ? <div className={styles.address}><dt>Адрес доставки</dt><dd>{order.deliveryAddress}</dd></div> : null}
      </dl>
      <div className={styles.actions}>
        <ButtonLink aria-label={`Подробнее о заказе ${order.number}`} href={href} prefetch={false} size="sm" variant="secondary">Подробнее</ButtonLink>
        <ButtonLink aria-label={`Повторить заказ ${order.number}`} href={`${href}/repeat`} prefetch={false} size="sm">
          <RepeatIcon aria-hidden="true" />Повторить
        </ButtonLink>
      </div>
    </div>
  </article>;
}

export function OrdersView({ initialSession }: { initialSession: RouteSession }) {
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
    content = <EmptyState
      actions={<>
        <LoginLink className={buttonClassName("primary")}>Войти</LoginLink>
        <Button onClick={() => void store.refresh()} variant="secondary">Проверить вход</Button>
      </>}
      icon={<ReceiptIcon />}
      title="Войдите, чтобы увидеть заказы"
    >
      История покупок доступна в вашем аккаунте.
    </EmptyState>;
  } else if (status === "unavailable" || status === "failed") {
    // A failure, not an empty history: a solid notice instead of the dashed empty state.
    content = <UnavailableState title="История заказов временно недоступна">
      <p>Не удалось проверить вход или загрузить заказы.</p>
      <Button onClick={() => void store.refresh()} variant="secondary">Повторить загрузку</Button>
    </UnavailableState>;
  } else if (owned?.status === "ready") {
    content = owned.orders.length ? <ul className={styles.list} aria-label="История заказов">
      {owned.orders.map((order) => <li key={order.id}><OrderSummary order={order} /></li>)}
    </ul> : <EmptyState
      actions={<ButtonLink href="/catalog">Перейти в каталог</ButtonLink>}
      icon={<ReceiptIcon />}
      title="Заказов пока нет"
    >
      После первой покупки здесь появится история заказов.
    </EmptyState>;
  } else content = <OrdersSkeleton />;

  return <main className={cartStyles.main} id="main-content"><Container>
    <PageHeader title="Мои заказы" subtitle="История покупок и повтор заказа с проверкой текущего наличия" />
    <div className={accountStyles.layout}>
      <AccountNav current="orders" />
      <div className={accountStyles.content}>{content}</div>
    </div>
  </Container></main>;
}

export function OrdersSkeleton() {
  return <div className={styles.list} aria-busy="true" aria-label="Загружаем историю заказов">
    <Skeleton className={styles.skeleton} variant="block" />
    <Skeleton className={styles.skeleton} variant="block" />
  </div>;
}
