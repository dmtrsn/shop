"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { failureMessages, type ApiFailure, type ApiResult, type MutationAuth } from "@/lib/admin-api";
import { formatRoubles } from "@/lib/money";
import { cancelOrder, changeOrderStatus, getOrder, type AdminOrder, type OrderStatus } from "@/lib/orders-api";
import { formatDateTime, OrderStatusBadge } from "./order-status";
import { useStaffApi } from "./use-staff-api";
import styles from "./admin-pages.module.css";

type Load = { status: "loading" } | { status: "ready"; order: AdminOrder } | { status: "error"; failure: ApiFailure };

export const orderConflictMessage = "Заказ был изменён другим сотрудником. Действие не выполнено.";
export const invalidTransitionMessage = "Это действие уже недоступно для заказа в его текущем состоянии. Действие не выполнено.";
export const cancelExplanation =
  "Заказ получит статус «Отменён», а заказанные товары вернутся на склад: остаток каждого товара увеличится на его количество в заказе. " +
  "Деньги автоматически не возвращаются, покупатель уведомление не получает, отменить отмену нельзя.";

// Forward actions Go may allow; cancellation has its own confirmed action.
const forwardActions: Partial<Record<OrderStatus, string>> = {
  transit: "Передать в доставку",
  delivered: "Отметить доставленным",
};

/**
 * One order for fulfilment. Lines and totals are the checkout snapshot. The
 * actions shown are exactly Go's allowedTransitions; every change sends the
 * revision this page shows, and a newer one is only adopted on explicit
 * reload.
 */
export function OrderDetail({ orderId }: { orderId: string }) {
  const router = useRouter();
  const { read, mutate } = useStaffApi();
  const [load, setLoad] = useState<Load>({ status: "loading" });
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ tone: "success" | "error"; text: string } | null>(null);
  const [blocked, setBlocked] = useState<string | null>(null);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const blockedRef = useRef<HTMLDivElement>(null);
  const confirmRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    void read(() => getOrder(orderId, controller.signal)).then((result) => {
      if (controller.signal.aborted) return;
      setLoad(result.ok ? { status: "ready", order: result.data } : { status: "error", failure: result.error });
    });
    return () => controller.abort();
  }, [orderId, read]);

  if (load.status === "loading") {
    return (
      <p aria-live="polite" className={styles.muted} role="status">
        Загрузка заказа…
      </p>
    );
  }
  if (load.status === "error") {
    return (
      <section className={styles.page}>
        <h1>{load.failure.kind === "not_found" ? "Заказ не найден" : "Не удалось открыть заказ"}</h1>
        <p className={styles.muted} role="alert">
          {load.failure.kind === "not_found" ? "Такого заказа нет." : failureMessages[load.failure.kind]}
        </p>
        <Link href="/orders">К списку заказов</Link>
      </section>
    );
  }
  const { order } = load;

  async function run(label: string, request: (auth: MutationAuth) => Promise<ApiResult<AdminOrder>>, success: (next: AdminOrder) => string) {
    setBusy(label);
    setNotice(null);
    setConfirmCancel(false);
    const result = await mutate(request);
    setBusy(null);
    if (result.ok) {
      setLoad({ status: "ready", order: result.data });
      setNotice({ tone: "success", text: success(result.data) });
      return;
    }
    const kind = result.error.kind;
    if (kind === "stale_revision" || kind === "invalid_transition") {
      setBlocked(kind === "stale_revision" ? orderConflictMessage : invalidTransitionMessage);
      requestAnimationFrame(() => blockedRef.current?.focus());
      return;
    }
    if (kind === "staff_changed") router.refresh();
    if (kind !== "unauthorized") setNotice({ tone: "error", text: kind === "conflict" ? invalidTransitionMessage : failureMessages[kind] });
  }

  async function reload() {
    setBusy("reload");
    const result = await read(() => getOrder(order.id));
    setBusy(null);
    if (!result.ok) {
      if (result.error.kind !== "unauthorized") setNotice({ tone: "error", text: failureMessages[result.error.kind] });
      return;
    }
    setLoad({ status: "ready", order: result.data });
    setBlocked(null);
    setNotice({ tone: "success", text: `Загружена актуальная версия. Ревизия ${result.data.revision}.` });
  }

  const forward = order.allowedTransitions.filter((status): status is "transit" | "delivered" => status in forwardActions);
  const cancellable = order.allowedTransitions.includes("cancelled");
  const total = order.items?.reduce((sum, line) => sum + line.lineTotalMinor, 0) ?? 0;

  return (
    <section aria-labelledby="order-title" className={styles.page}>
      <p className={styles.breadcrumb}>
        <Link href="/orders">Заказы</Link>
      </p>
      <div className={styles.heading}>
        <h1 id="order-title">Заказ {order.number}</h1>
        <p>
          <span className={styles.visuallyHidden}>Статус: </span>
          <OrderStatusBadge order={order} />
        </p>
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>ID</dt>
          <dd>{order.id}</dd>
        </div>
        <div>
          <dt>Ревизия</dt>
          <dd data-testid="order-revision">{order.revision}</dd>
        </div>
        <div>
          <dt>Создан</dt>
          <dd>{formatDateTime(order.createdAt)}</dd>
        </div>
        {order.deliveredAt ? (
          <div>
            <dt>Доставлен</dt>
            <dd>{formatDateTime(order.deliveredAt)}</dd>
          </div>
        ) : null}
      </dl>

      <div aria-live="polite" role="status">
        {notice?.tone === "success" ? <p className={styles.success}>{notice.text}</p> : null}
      </div>
      {notice?.tone === "error" ? (
        <p className={styles.alert} role="alert">
          {notice.text}
        </p>
      ) : null}
      {blocked ? (
        <div className={styles.conflict} ref={blockedRef} role="alert" tabIndex={-1}>
          <p>
            <strong>{blocked}</strong>
          </p>
          <p>Показанные данные могли устареть. Загрузите актуальную версию заказа, прежде чем менять его.</p>
          <button className={styles.primaryButton} disabled={busy === "reload"} onClick={reload} type="button">
            {busy === "reload" ? "Загружаем…" : "Загрузить актуальную версию"}
          </button>
        </div>
      ) : null}

      <div className={styles.columns}>
        <section aria-labelledby="order-customer" className={styles.card}>
          <h2 id="order-customer">Покупатель</h2>
          <p className={styles.wrap}>{order.customer.name}</p>
          <p className={styles.wrap}>
            <a href={`mailto:${order.customer.email}`}>{order.customer.email}</a>
          </p>
          {order.customer.phone ? <p>{order.customer.phone}</p> : null}
          <p className={styles.hint}>Текущие данные учётной записи покупателя.</p>
        </section>
        <section aria-labelledby="order-delivery" className={styles.card}>
          <h2 id="order-delivery">Доставка</h2>
          <p className={styles.wrap}>{order.deliveryAddress}</p>
          {order.comment ? (
            <p className={styles.wrap}>
              <span className={styles.muted}>Комментарий: </span>
              {order.comment}
            </p>
          ) : null}
        </section>
      </div>

      <section aria-labelledby="order-lines">
        <h2 id="order-lines" className={styles.sectionTitle}>
          Состав заказа ({order.itemCount} шт.)
        </h2>
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Товар</th>
                <th className={styles.secondary} scope="col">
                  Производитель
                </th>
                <th className={styles.number} scope="col">
                  Количество
                </th>
                <th className={styles.number} scope="col">
                  Цена
                </th>
                <th className={styles.number} scope="col">
                  Сумма
                </th>
              </tr>
            </thead>
            <tbody>
              {order.items?.map((line) => (
                <tr key={line.position}>
                  <td>
                    {line.name}
                    <span className={styles.meta}>
                      {line.sku} · {line.productId}
                    </span>
                  </td>
                  <td className={styles.secondary}>{line.manufacturer}</td>
                  <td className={styles.number}>{line.quantity}</td>
                  <td className={styles.number}>{formatRoubles(line.unitPriceMinor)}</td>
                  <td className={styles.number}>{formatRoubles(line.lineTotalMinor)}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <th colSpan={3} scope="row">
                  Товары
                </th>
                <td className={styles.number} colSpan={2}>
                  {formatRoubles(total)}
                </td>
              </tr>
              {order.totalMinor !== total ? (
                <tr>
                  <th colSpan={3} scope="row">
                    Доставка
                  </th>
                  <td className={styles.number} colSpan={2}>
                    {formatRoubles(order.totalMinor - total)}
                  </td>
                </tr>
              ) : null}
              <tr>
                <th colSpan={3} scope="row">
                  Итого
                </th>
                <td className={styles.number} colSpan={2}>
                  <strong>{formatRoubles(order.totalMinor)}</strong>
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </section>

      <section aria-labelledby="order-actions" className={styles.availability}>
        <h2 id="order-actions">Действия</h2>
        {forward.length === 0 && !cancellable ? (
          <p>
            {order.status === "delivered"
              ? "Заказ доставлен — дальнейших действий нет."
              : order.status === "cancelled"
                ? "Заказ отменён — дальнейших действий нет."
                : "Для заказа в этом статусе действий нет."}
          </p>
        ) : null}
        {forward.length ? (
          <div className={styles.actions}>
            {forward.map((status) => (
              <button
                className={styles.primaryButton}
                disabled={busy !== null || blocked !== null}
                key={status}
                onClick={() =>
                  run(status, (auth) => changeOrderStatus(order.id, order.revision, status, auth), (next) => `Статус изменён: «${next.statusLabel}». Ревизия ${next.revision}.`)
                }
                type="button"
              >
                {busy === status ? "Сохраняем…" : forwardActions[status]}
              </button>
            ))}
          </div>
        ) : null}
        {cancellable ? (
          confirmCancel ? (
            <div aria-labelledby="cancel-confirm-title" className={styles.confirm} role="group">
              <h3 id="cancel-confirm-title" ref={confirmRef} tabIndex={-1}>
                Отменить заказ {order.number}?
              </h3>
              <p>{cancelExplanation}</p>
              <div className={styles.actions}>
                <button
                  className={styles.dangerButton}
                  onClick={() =>
                    run("cancel", (auth) => cancelOrder(order.id, order.revision, auth), (next) => `Заказ отменён, товары возвращены на склад. Ревизия ${next.revision}.`)
                  }
                  type="button"
                >
                  Отменить заказ
                </button>
                <button className={styles.secondaryAction} onClick={() => setConfirmCancel(false)} type="button">
                  Не отменять
                </button>
              </div>
            </div>
          ) : (
            <button
              className={styles.secondaryAction}
              disabled={busy !== null || blocked !== null}
              onClick={() => {
                setConfirmCancel(true);
                requestAnimationFrame(() => confirmRef.current?.focus());
              }}
              type="button"
            >
              {busy === "cancel" ? "Отменяем…" : "Отменить заказ…"}
            </button>
          )
        ) : null}
      </section>
    </section>
  );
}
