"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

import { Button, ButtonLink, buttonClassName } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { EmptyState } from "@/components/ui/empty-state";
import { ArrowRightIcon, InfoIcon, ReceiptIcon, RepeatIcon } from "@/components/ui/icons";
import { PageHeader } from "@/components/ui/page-header";
import { BrowserRequestError, ResponseOwnerError } from "@/lib/api/browser-request";
import type { RouteSession } from "@/lib/api/browser-session";
import type { Order } from "@/lib/api/order";
import { getBrowserOrder } from "@/lib/api/order-client";
import { formatMoney } from "@/lib/money";
import { orderStatusTone } from "@/lib/order-status";
import { pluralRu } from "@/lib/plural";

import { useBrowserSession, useBrowserSessionStore } from "./browser-session-provider";
import { useCart } from "./cart-provider";
import { LoginLink } from "./login-link";
import cartStyles from "./cart.module.css";
import { OrderThumb } from "./order-thumb";
import styles from "./orders.module.css";

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
      <PageHeader title={`Заказ ${order.number}`} subtitle={`Оформлен ${dateTime(order.createdAt)}`} />
      <div className={styles.detailLayout}>
        <div className={styles.detailMain}>
          <section aria-label="Сведения о заказе" className={styles.panel}>
            <dl className={styles.detailFacts}>
              <div><dt>Статус</dt><dd>
                {/* Go's label is the status; tone only reinforces it. */}
                <span className={`${styles.status} ${orderStatusTone(order.status) === "success" ? styles.statusSuccess : ""}`}>{order.statusLabel || order.status}</span>
              </dd></div>
              {order.deliveredAt ? <div><dt>Доставлен</dt><dd>{dateTime(order.deliveredAt)}</dd></div> : null}
              <div><dt>Адрес доставки</dt><dd>{order.deliveryAddress}</dd></div>
              {order.comment ? <div><dt>Комментарий</dt><dd>{order.comment}</dd></div> : null}
            </dl>
          </section>
          <section aria-labelledby="order-items-title" className={styles.panel}>
            <h2 id="order-items-title">Товары</h2>
            <ul className={styles.rows}>
              {order.items.map((item, index) => (
                <li className={styles.row} key={`${item.productId}-${index}`}>
                  {/* Snapshot image from the order itself; never enriched from the current catalog. */}
                  <OrderThumb image={item.image} />
                  <div className={styles.rowIdentity}>
                    {/* The snapshot has no slug; Go resolves the ID and redirects. */}
                    <p className={styles.rowName}><Link href={`/products/${encodeURIComponent(item.productId)}`}>{item.name}</Link></p>
                    <p className={cartStyles.meta}>Арт. {item.sku}{item.manufacturer ? ` · ${item.manufacturer}` : ""}</p>
                  </div>
                  <dl className={styles.rowFigures}>
                    <div><dt>Количество</dt><dd>{item.quantity}</dd></div>
                    <div><dt>Цена</dt><dd>{formatMoney(item.unitPriceMinor, currency)}</dd></div>
                    <div><dt>Сумма</dt><dd>{formatMoney(item.lineTotalMinor, currency)}</dd></div>
                  </dl>
                </li>
              ))}
            </ul>
          </section>
        </div>
        <aside aria-labelledby="order-total-title" className={cartStyles.summary}>
          <h2 id="order-total-title">Сумма заказа</h2>
          <dl>
            <div><dt>Товаров</dt><dd>{units(order.itemCount)}</dd></div>
            <div className={cartStyles.total}><dt>Итого с доставкой</dt><dd>{formatMoney(order.totalMinor, currency)}</dd></div>
          </dl>
          <div className={styles.summaryActions}>
            <ButtonLink href={`/account/orders/${encodeURIComponent(order.id)}/repeat`} prefetch={false} variant="brand"><RepeatIcon aria-hidden="true" />Повторить заказ</ButtonLink>
          </div>
          <p className={cartStyles.note}>Названия и цены сохранены на момент оформления.</p>
          <div className={cartStyles.actions}>
            <Link className={cartStyles.secondaryLink} href="/catalog">Продолжить покупки</Link>
            <Link className={cartStyles.secondaryLink} href="/cart">Корзина</Link>
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
      <PageHeader title="Заказ" />
      <EmptyState
        actions={<>
          <LoginLink className={buttonClassName("primary")}>Войти</LoginLink>
          <Button onClick={() => void store.refresh()} variant="secondary">Проверить вход</Button>
        </>}
        icon={<ReceiptIcon />}
        title="Войдите, чтобы открыть заказ"
      >
        Заказы доступны в вашем аккаунте.
      </EmptyState>
    </>;
  } else if (status === "unavailable" || status === "failed") {
    content = <>
      <PageHeader title="Заказ" />
      <section className={cartStyles.unavailableState}>
        <InfoIcon aria-hidden="true" />
        <div>
          <h2>Заказ временно недоступен</h2>
          <p>Не удалось проверить вход или загрузить заказ.</p>
          <Button onClick={() => void store.refresh()} variant="secondary">Повторить загрузку</Button>
        </div>
      </section>
    </>;
  } else if (status === "notFound") {
    content = <>
      <PageHeader title="Заказ не найден" />
      <EmptyState
        actions={<>
          <ButtonLink href="/catalog" variant="secondary">Перейти в каталог</ButtonLink>
          <ButtonLink href="/cart" variant="tertiary">Корзина</ButtonLink>
        </>}
        icon={<ReceiptIcon />}
        title="Такого заказа нет в этом аккаунте"
      >
        Проверьте ссылку. Заказы другого аккаунта здесь не показываются.
      </EmptyState>
    </>;
  } else {
    content = <>
      <PageHeader title="Заказ" />
      <OrderSkeleton />
    </>;
  }
  return <main className={cartStyles.main} id="main-content"><Container>
    <Link className={styles.back} href="/account/orders" prefetch={false}><ArrowRightIcon aria-hidden="true" />Все заказы</Link>
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
