"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { EmptyState } from "@/components/ui/empty-state";
import { PageHeader } from "@/components/ui/page-header";
import { SearchInput } from "@/components/ui/search-input";
import type { Manufacturer } from "@/lib/api/catalog";
import type { Product } from "@/lib/api/product-dto";
import { loadManufacturerProducts } from "@/lib/api/quick-order-client";
import { formatMoney } from "@/lib/money";
import { pluralRu } from "@/lib/plural";
import { compareManufacturers, compareProducts, matchesProduct } from "@/lib/quick-order";

import { useCart } from "./cart-provider";
import { LoginLink } from "./login-link";
import { QuickOrderManufacturerChooser } from "./quick-order-manufacturer-chooser";
import { QuickOrderRow } from "./quick-order-row";
import styles from "./quick-order.module.css";

export type QuickOrderCart = ReturnType<typeof useCart>;

// Keyed by manufacturer: returning never resurrects query, scroll or drafts.
function Products({ manufacturer, cart }: { manufacturer: Manufacturer; cart: QuickOrderCart }) {
  const [query, setQuery] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<{ status: "loading" | "ready" | "error"; products: Product[] }>({ status: "loading", products: [] });
  const generation = useRef(0);
  const queryGeneration = useRef(0);
  const search = useRef<HTMLInputElement>(null);
  const panel = useRef<HTMLElement>(null);
  const byID = useMemo(() => new Map(cart.cart?.items.map((line) => [line.product.id, line]) ?? []), [cart.cart]);

  useEffect(() => {
    const current = ++generation.current;
    const controller = new AbortController();
    // The keyed replacement starts its results list at the top.
    if (panel.current) {
      // On mobile the document scrolls rather than the panel. Back/Forward can
      // replace a long list while the viewport is still deep inside it.
      const top = panel.current.getBoundingClientRect().top;
      const headerHeight = document.querySelector("body > header")?.getBoundingClientRect().height ?? 0;
      if (top < headerHeight) window.scrollTo({ top: Math.max(0, window.scrollY + top - headerHeight), behavior: "instant" });
    }
    void loadManufacturerProducts(manufacturer.id, { signal: controller.signal }).then((products) => {
      if (generation.current === current && !controller.signal.aborted) setLoad({ status: "ready", products });
    }, () => {
      if (generation.current === current && !controller.signal.aborted) setLoad({ status: "error", products: [] });
    });
    return () => { ++generation.current; controller.abort(); };
  }, [manufacturer.id, attempt]);

  const focusAfterSubmit = useCallback(() => {
    const request = generation.current;
    const queryVersion = queryGeneration.current;
    return () => {
      if (request !== generation.current || queryVersion !== queryGeneration.current) return;
      search.current?.focus();
      search.current?.select();
    };
  }, []);

  const refreshProduct = useCallback((product: Product) => {
    setLoad((previous) => ({ ...previous, products: previous.products.flatMap((old) => old.id === product.id
      ? product.manufacturer.id === manufacturer.id ? [product] : [] : [old]).sort(compareProducts) }));
  }, [manufacturer.id]);
  const visible = useMemo(() => load.products.filter((product) => matchesProduct(product, query)), [load.products, query]);
  const visibleIDs = useMemo(() => new Set(visible.map((product) => product.id)), [visible]);
  return <section aria-label={`Товары: ${manufacturer.name}`} className={styles.products} ref={panel}>
    <h2>{manufacturer.name}</h2>
    <SearchInput label="Поиск товара" placeholder="Название или артикул" ref={search} value={query}
      onChange={(event) => { ++queryGeneration.current; setQuery(event.target.value); }} />
    <div className={styles.productResults}>
    {load.status === "loading" ? <p role="status" aria-busy="true">Загружаем товары производителя…</p> : null}
    {load.status === "error" ? <div role="alert"><p>Не удалось загрузить товары производителя.</p>
      <Button onClick={() => { setLoad({ status: "loading", products: [] }); setAttempt((value) => value + 1); }} variant="secondary">Повторить загрузку товаров</Button></div> : null}
    {load.status === "ready" ? <>
      <p className="visually-hidden" role="status">Найдено товаров: {visible.length}</p>
      <ol className={styles.rows} hidden={!visible.length}>
        {load.products.map((product) => <QuickOrderRow hidden={!visibleIDs.has(product.id)} key={`${cart.userID ?? "public"}:${product.id}`} product={product} cart={cart} line={byID.get(product.id)}
          prepareFocus={focusAfterSubmit} refreshProduct={refreshProduct} />)}
      </ol>
      {!visible.length ? <EmptyState title={load.products.length ? "Ничего не найдено у этого производителя" : "У этого производителя пока нет товаров"} level={3} /> : null}
    </> : null}
    </div>
  </section>;
}

export function QuickOrderView({ manufacturers }: { manufacturers: Manufacturer[] }) {
  const params = useSearchParams();
  const slug = params.get("manufacturer") ?? "";
  const sorted = useMemo(() => [...manufacturers].sort(compareManufacturers), [manufacturers]);
  const selected = sorted.find((manufacturer) => manufacturer.slug === slug);
  const cart = useCart();
  const { ensure } = cart;
  useEffect(() => { void ensure(); }, [ensure]);

  function select(manufacturer: Manufacturer) {
    if (manufacturer.slug === slug) return;
    // Next integrates native history with useSearchParams, including Back/Forward.
    const url = new URL(window.location.href);
    url.search = new URLSearchParams({ manufacturer: manufacturer.slug }).toString();
    url.hash = "";
    window.history.pushState(null, "", url);
  }
  const summary = cart.cart?.summary;
  return <main className={styles.main} id="main-content">
    <Container>
      <PageHeader title="Быстрый заказ" subtitle="Поиск товаров по производителям" />
      <p className={styles.intro}>Выберите производителя, найдите товар и сразу укажите количество.</p>
      <div aria-label="Общая корзина" className={styles.summary}>
        {summary ? <Link href="/cart" prefetch={false}>В корзине: {summary.positionCount} {pluralRu(summary.positionCount, ["позиция", "позиции", "позиций"])} · К оплате: {formatMoney(summary.totalMinor, "RUB")}</Link>
          : cart.status === "anonymous" ? <LoginLink>Войти, чтобы купить</LoginLink>
            : cart.status === "unavailable" ? <><span>Корзина недоступна.</span> <Button onClick={() => void cart.refresh()} size="sm" variant="secondary">Повторить проверку корзины</Button></>
              : <span role="status">Загружаем корзину…</span>}
        {cart.reconciling ? <span role="status">Проверяем сохранённую корзину…</span> : null}
      </div>
      <div className={styles.layout}>
        <QuickOrderManufacturerChooser manufacturers={sorted} selected={selected?.slug} onSelect={select} />
        {selected ? <Products cart={cart} key={selected.slug} manufacturer={selected} />
          : <EmptyState title="Выберите производителя">{slug ? "Этот производитель не найден. Выберите производителя из списка." : null}</EmptyState>}
      </div>
    </Container>
  </main>;
}
