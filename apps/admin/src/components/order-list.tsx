"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";

import { failureMessages } from "@/lib/admin-api";
import { formatRoubles } from "@/lib/money";
import { orderListHref } from "@/lib/order-query";
import { listOrders, orderStatusLabels, orderStatuses, type OrderPage, type OrderQuery, type OrderStatus } from "@/lib/orders-api";
import { pageSizes } from "@/lib/product-query";
import { formatDateTime, OrderStatusBadge } from "./order-status";
import { useStaffApi } from "./use-staff-api";
import styles from "./admin-pages.module.css";

type State = { status: "loading" } | { status: "ready"; page: OrderPage } | { status: "error"; message: string };

/** The staff order list; the URL holds the query and Go does the filtering. */
export function OrderList({ query }: { query: OrderQuery }) {
  const router = useRouter();
  const { read } = useStaffApi();
  const [state, setState] = useState<State>({ status: "loading" });
  const [search, setSearch] = useState(query.q);
  const [reload, setReload] = useState(0);

  useEffect(() => setSearch(query.q), [query.q]);

  const queryKey = orderListHref(query);
  useEffect(() => {
    const controller = new AbortController();
    setState({ status: "loading" });
    void read(() => listOrders(query, controller.signal)).then((result) => {
      if (controller.signal.aborted) return;
      setState(result.ok ? { status: "ready", page: result.data } : { status: "error", message: failureMessages[result.error.kind] });
    });
    return () => controller.abort();
  }, [queryKey, read, reload]);

  const go = (next: Partial<OrderQuery>) => router.push(orderListHref({ ...query, page: 1, ...next }));

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    go({ q: search.trim() });
  }

  return (
    <section aria-labelledby="orders-title" className={styles.page}>
      <div className={styles.heading}>
        <h1 id="orders-title">Заказы</h1>
      </div>
      <form className={styles.filters} onSubmit={submit} role="search">
        <div className={styles.filterField}>
          <label htmlFor="order-search">Поиск</label>
          <input
            id="order-search"
            maxLength={100}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Номер или ID заказа, email или имя покупателя"
            type="search"
            value={search}
          />
        </div>
        <div className={styles.filterField}>
          <label htmlFor="order-status">Статус</label>
          <select id="order-status" onChange={(event) => go({ status: event.target.value as OrderStatus | "" })} value={query.status}>
            <option value="">Все</option>
            {orderStatuses.map((status) => (
              <option key={status} value={status}>
                {orderStatusLabels[status]}
              </option>
            ))}
          </select>
        </div>
        <div className={styles.filterField}>
          <label htmlFor="order-limit">На странице</label>
          <select id="order-limit" onChange={(event) => go({ limit: Number(event.target.value) })} value={query.limit}>
            {pageSizes.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </div>
        <button className={styles.secondaryAction} type="submit">
          Найти
        </button>
      </form>
      <Results onRetry={() => setReload((value) => value + 1)} query={query} state={state} />
    </section>
  );
}

function Results({ state, query, onRetry }: { state: State; query: OrderQuery; onRetry: () => void }) {
  if (state.status === "loading") {
    return (
      <p aria-live="polite" className={styles.muted} role="status">
        Загрузка заказов…
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <div className={styles.alert} role="alert">
        <p>Не удалось загрузить заказы. {state.message}</p>
        <button className={styles.secondaryAction} onClick={onRetry} type="button">
          Повторить
        </button>
      </div>
    );
  }
  const { page } = state;
  if (page.items.length === 0) {
    const beyond = page.total > 0 && query.page > page.totalPages;
    return (
      <div className={styles.empty} role="status">
        <p>{beyond ? "На этой странице заказов нет." : query.q || query.status ? "Ничего не найдено." : "Заказов нет."}</p>
        {beyond ? <Link href={orderListHref({ ...query, page: 1 })}>К первой странице</Link> : null}
      </div>
    );
  }
  return (
    <>
      <p aria-live="polite" className={styles.muted} role="status">
        Найдено: {page.total}
      </p>
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <caption className={styles.visuallyHidden}>Заказы, страница {page.page}</caption>
          <thead>
            <tr>
              <th scope="col">Заказ</th>
              <th scope="col">Создан</th>
              <th className={styles.secondary} scope="col">
                Покупатель
              </th>
              <th scope="col">Статус</th>
              <th className={styles.number} scope="col">
                Сумма
              </th>
              <th className={`${styles.number} ${styles.secondary}`} scope="col">
                Товаров
              </th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((order) => (
              <tr key={order.id}>
                <td>
                  <Link className={styles.productLink} href={`/orders/${encodeURIComponent(order.id)}`}>
                    Заказ {order.number}
                  </Link>
                  <span className={styles.meta}>{order.id}</span>
                </td>
                <td>{formatDateTime(order.createdAt)}</td>
                <td className={styles.secondary}>
                  {order.customer.name}
                  <span className={styles.meta}>{order.customer.email}</span>
                </td>
                <td>
                  <OrderStatusBadge order={order} />
                </td>
                <td className={styles.number}>{formatRoubles(order.totalMinor)}</td>
                <td className={`${styles.number} ${styles.secondary}`}>{order.itemCount}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <nav aria-label="Страницы списка" className={styles.pagination}>
        {page.page > 1 ? <Link href={orderListHref({ ...query, page: page.page - 1 })}>← Назад</Link> : <span aria-hidden="true" />}
        <span>
          Страница {page.page} из {page.totalPages}
        </span>
        {page.page < page.totalPages ? <Link href={orderListHref({ ...query, page: page.page + 1 })}>Вперёд →</Link> : <span aria-hidden="true" />}
      </nav>
    </>
  );
}
