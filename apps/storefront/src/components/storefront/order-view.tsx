"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { BrowserRequestError, ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Order } from "@/lib/api/order";
import { getBrowserOrder } from "@/lib/api/order-client";
import { formatMoney } from "@/lib/money";
import { pluralRu } from "@/lib/plural";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import { useCart } from "./cart-provider";
import { LoginLink } from "./login-link";
import cartStyles from "./cart.module.css";
import styles from "./checkout.module.css";

// Orders carry no currency; every storefront price is in rubles.
const currency = "RUB";
const units = (count: number) => `${count} ${pluralRu(count, ["единица", "единицы", "единиц"])}`;
const dateTime = (value: string) => new Intl.DateTimeFormat("ru-RU", { dateStyle: "long", timeStyle: "short" }).format(new Date(value));

// A result is published only for the user and session generation that read it.
type Loaded = { owner: string; generation: number } & (
  | { status: "ready"; order: Order }
  | { status: "notFound" | "failed" }
);

function OrderDetails({ order, placed }: { order: Order; placed: boolean }) {
  useEffect(() => {
    if (placed) document.getElementById("order-placed")?.focus();
  }, [placed]);
  return (
    <>
      {placed ? (
        <p className={styles.banner} id="order-placed" role="status" tabIndex={-1}>Заказ {order.number} оформлен</p>
      ) : null}
      <header className={cartStyles.heading}>
        <h1>Заказ {order.number}</h1>
        <p>Оформлен {dateTime(order.createdAt)}</p>
      </header>
      <div className={styles.layout}>
        <div className={styles.details}>
          <dl className={styles.facts}>
            <dt>Статус</dt><dd>{order.statusLabel || order.status}</dd>
            {order.deliveredAt ? <><dt>Доставлен</dt><dd>{dateTime(order.deliveredAt)}</dd></> : null}
            <dt>Адрес доставки</dt><dd>{order.deliveryAddress}</dd>
            {order.comment ? <><dt>Комментарий</dt><dd>{order.comment}</dd></> : null}
          </dl>
          <section aria-labelledby="order-items-title">
            <h2 id="order-items-title">Товары</h2>
            <ul className={styles.lines}>
              {order.items.map((item, index) => (
                <li className={styles.line} key={`${item.productId}-${index}`}>
                  <div className={styles.lineIdentity}>
                    {/* The snapshot has no slug; Go resolves the ID and redirects. */}
                    <p className={styles.lineName}><Link href={`/products/${encodeURIComponent(item.productId)}`}>{item.name}</Link></p>
                    <p className={cartStyles.meta}>Арт. {item.sku}{item.manufacturer ? ` · ${item.manufacturer}` : ""}</p>
                  </div>
                  <dl className={styles.lineFigures}>
                    <div><dt>Количество</dt><dd>{item.quantity}</dd></div>
                    <div><dt>Цена</dt><dd>{formatMoney(item.unitPriceMinor, currency)}</dd></div>
                    <div><dt>Сумма</dt><dd>{formatMoney(item.lineTotalMinor, currency)}</dd></div>
                  </dl>
                </li>
              ))}
            </ul>
          </section>
        </div>
        <aside aria-labelledby="order-total-title" className={`${cartStyles.summary} ${styles.summary}`}>
          <h2 id="order-total-title">Сумма заказа</h2>
          <dl>
            <div><dt>Товаров</dt><dd>{units(order.itemCount)}</dd></div>
            <div className={cartStyles.total}><dt>Итого с доставкой</dt><dd>{formatMoney(order.totalMinor, currency)}</dd></div>
          </dl>
          <Link className={cartStyles.secondaryLink} href={`/account/orders/${encodeURIComponent(order.id)}/repeat`} prefetch={false}>Повторить заказ</Link>
          <p className={cartStyles.note}>Названия и цены сохранены на момент оформления.</p>
          <div className={cartStyles.actions}>
            <Link href="/catalog">Продолжить покупки</Link>
            <Link href="/cart">Корзина</Link>
          </div>
        </aside>
      </div>
    </>
  );
}

export function OrderView({ initialSession, orderID }: { initialSession: RouteSession; orderID: string }) {
  const store = useBrowserSessionStore();
  const session = useBrowserSession();
  const { takePlacedOrder } = useCart();
  const routeKey = `${initialSession.status === "authenticated" ? `user:${initialSession.userID}` : initialSession.status}:${orderID}`;
  const [enteredKey, setEnteredKey] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [placed, setPlaced] = useState<{ userID: string; orderID: string } | null>(null);

  // The order route needs only the shared session. Cart is not initialized
  // here; an already initialized Cart provider rereads on the session change.
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
    getBrowserOrder(orderID, { expectedUserID: userID, signal: controller.signal }).then((order) => {
      if (current()) setLoaded({ status: "ready", order, owner: userID, generation });
    }, async (error: unknown) => {
      if (!current()) return;
      if (error instanceof ResponseOwnerError) await store.recoverOwner(generation);
      else if (error instanceof BrowserRequestError && error.status === 401) store.invalidate("anonymous");
      else if (error instanceof BrowserRequestError && error.status === 404) setLoaded({ status: "notFound", owner: userID, generation });
      else setLoaded({ status: "failed", owner: userID, generation });
    });
    return () => { active = false; controller.abort(); };
  }, [generation, orderID, store, userID]);

  const owned = loaded && userID !== null && loaded.owner === userID && loaded.generation === generation ? loaded : null;
  const order = owned?.status === "ready" ? owned.order : null;
  // The banner is shown only when this tab just created this order.
  useEffect(() => {
    if (order && userID && takePlacedOrder(userID, order.id)) setPlaced({ userID, orderID: order.id });
  }, [order, takePlacedOrder, userID]);

  const status = !entered ? (initialSession.status === "authenticated" ? "loading" : initialSession.status)
    : session.status === "anonymous" || session.status === "unavailable" ? session.status
      : owned ? owned.status : "loading";
  let content;
  if (order) {
    content = <OrderDetails order={order} placed={placed?.userID === userID && placed.orderID === order.id} />;
  } else if (status === "anonymous") {
    content = <>
      <header className={cartStyles.heading}><h1>Заказ</h1></header>
      <section className={cartStyles.state}>
        <h2>Войдите, чтобы открыть заказ</h2>
        <p>Заказы доступны в вашем аккаунте.</p>
        <LoginLink>Войти</LoginLink>
        <Button onClick={() => void store.refresh()} variant="secondary">Проверить вход</Button>
      </section>
    </>;
  } else if (status === "unavailable" || status === "failed") {
    content = <>
      <header className={cartStyles.heading}><h1>Заказ</h1></header>
      <section className={cartStyles.state}>
        <h2>Заказ временно недоступен</h2>
        <p>Не удалось проверить вход или загрузить заказ.</p>
        <Button onClick={() => void store.refresh()} variant="secondary">Повторить загрузку</Button>
      </section>
    </>;
  } else if (status === "notFound") {
    content = <>
      <header className={cartStyles.heading}><h1>Заказ не найден</h1></header>
      <section className={cartStyles.state}>
        <p>Проверьте ссылку. Заказы другого аккаунта здесь не показываются.</p>
        <div className={cartStyles.actions}><Link href="/catalog">Перейти в каталог</Link><Link href="/cart">Корзина</Link></div>
      </section>
    </>;
  } else {
    content = <>
      <header className={cartStyles.heading}><h1>Заказ</h1></header>
      <OrderSkeleton />
    </>;
  }
  return <main className={cartStyles.main} id="main-content"><Container>
    <Link className={cartStyles.secondaryLink} href="/account/orders" prefetch={false}>Все заказы</Link>
    {content}
  </Container></main>;
}

export function OrderSkeleton() {
  return (
    <div aria-busy="true" aria-label="Загружаем заказ" className={cartStyles.layout}>
      <div aria-hidden="true" className={cartStyles.skeletonRow} />
      <div aria-hidden="true" className={cartStyles.skeletonSummary} />
    </div>
  );
}
