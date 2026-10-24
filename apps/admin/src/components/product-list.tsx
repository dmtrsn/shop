"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";

import { failureMessages } from "@/lib/admin-api";
import { formatRoubles } from "@/lib/money";
import { listProducts, type ProductPage, type ProductQuery, type ProductStatus } from "@/lib/products-api";
import { pageSizes, productListHref } from "@/lib/product-query";
import { useStaffApi } from "./use-staff-api";
import styles from "./products.module.css";

type State = { status: "loading" } | { status: "ready"; page: ProductPage } | { status: "error"; message: string };

const statusLabels: Record<ProductStatus, string> = { active: "В продаже", archived: "В архиве", all: "Все" };

/**
 * The staff product list. The URL holds the query (search, status, page,
 * size), so reloads, back/forward and shared links show the same list; Go
 * does the searching.
 */
export function ProductList({ query }: { query: ProductQuery }) {
  const router = useRouter();
  const { read } = useStaffApi();
  const [state, setState] = useState<State>({ status: "loading" });
  const [search, setSearch] = useState(query.q);
  const [reload, setReload] = useState(0);

  useEffect(() => setSearch(query.q), [query.q]);

  // Keyed by the canonical URL, so equal queries never refetch.
  const queryKey = productListHref(query);
  useEffect(() => {
    const controller = new AbortController();
    setState({ status: "loading" });
    void read(() => listProducts(query, controller.signal)).then((result) => {
      if (controller.signal.aborted) return;
      setState(result.ok ? { status: "ready", page: result.data } : { status: "error", message: failureMessages[result.error.kind] });
    });
    return () => controller.abort();
  }, [queryKey, read, reload]);

  const go = (next: Partial<ProductQuery>) => router.push(productListHref({ ...query, page: 1, ...next }));

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    go({ q: search.trim() });
  }

  return (
    <section aria-labelledby="products-title" className={styles.page}>
      <div className={styles.heading}>
        <h1 id="products-title">Товары</h1>
        <Link className={styles.primaryAction} href="/products/new">
          Создать товар
        </Link>
      </div>
      <form className={styles.filters} onSubmit={submit} role="search">
        <div className={styles.filterField}>
          <label htmlFor="product-search">Поиск</label>
          <input
            id="product-search"
            maxLength={100}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Название, артикул, slug или ID"
            type="search"
            value={search}
          />
        </div>
        <div className={styles.filterField}>
          <label htmlFor="product-status">Статус</label>
          <select id="product-status" onChange={(event) => go({ status: event.target.value as ProductStatus })} value={query.status}>
            {(Object.keys(statusLabels) as ProductStatus[]).map((status) => (
              <option key={status} value={status}>
                {statusLabels[status]}
              </option>
            ))}
          </select>
        </div>
        <div className={styles.filterField}>
          <label htmlFor="product-limit">На странице</label>
          <select id="product-limit" onChange={(event) => go({ limit: Number(event.target.value) })} value={query.limit}>
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

function Results({ state, query, onRetry }: { state: State; query: ProductQuery; onRetry: () => void }) {
  if (state.status === "loading") {
    return (
      <p aria-live="polite" className={styles.muted} role="status">
        Загрузка товаров…
      </p>
    );
  }
  if (state.status === "error") {
    return (
      <div className={styles.alert} role="alert">
        <p>Не удалось загрузить товары. {state.message}</p>
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
        <p>{beyond ? "На этой странице товаров нет." : query.q ? "Ничего не найдено." : "Товаров нет."}</p>
        {beyond ? <Link href={productListHref({ ...query, page: 1 })}>К первой странице</Link> : null}
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
          <caption className={styles.visuallyHidden}>Товары, страница {page.page}</caption>
          <thead>
            <tr>
              <th scope="col">Товар</th>
              <th className={styles.number} scope="col">
                Цена
              </th>
              <th className={styles.number} scope="col">
                Остаток
              </th>
              <th className={styles.secondary} scope="col">
                Категория и производитель
              </th>
              <th scope="col">Статус</th>
              <th className={`${styles.number} ${styles.secondary}`} scope="col">
                Ревизия
              </th>
            </tr>
          </thead>
          <tbody>
            {page.items.map((product) => (
              <tr key={product.id}>
                <td>
                  <Link className={styles.productLink} href={`/products/${encodeURIComponent(product.id)}`}>
                    {product.name}
                  </Link>
                  <span className={styles.meta}>
                    {product.sku} · {product.id}
                  </span>
                </td>
                <td className={styles.number}>{formatRoubles(product.priceMinor)}</td>
                <td className={styles.number}>{product.stock}</td>
                <td className={styles.secondary}>
                  {product.category.name}
                  <span className={styles.meta}>{product.manufacturer.name}</span>
                </td>
                <td>
                  <span className={product.archived ? styles.statusArchived : styles.statusActive}>{product.archived ? "В архиве" : "В продаже"}</span>
                </td>
                <td className={`${styles.number} ${styles.secondary}`}>{product.revision}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <nav aria-label="Страницы списка" className={styles.pagination}>
        {page.page > 1 ? <Link href={productListHref({ ...query, page: page.page - 1 })}>← Назад</Link> : <span aria-hidden="true" />}
        <span>
          Страница {page.page} из {page.totalPages}
        </span>
        {page.page < page.totalPages ? <Link href={productListHref({ ...query, page: page.page + 1 })}>Вперёд →</Link> : <span aria-hidden="true" />}
      </nav>
    </>
  );
}
