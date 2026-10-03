"use client";

import Link from "next/link";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Container } from "@/components/ui/container";

import { useFavorites, type FavoritesRouteSession } from "./favorites-provider";
import { ProductCard } from "./product-card";
import styles from "./favorites.module.css";

export function FavoritesSkeleton() {
  return <div aria-busy="true" aria-label="Загружаем избранное" className={styles.grid}>
    {Array.from({ length: 4 }, (_, index) => <div aria-hidden="true" className={styles.skeleton} key={index} />)}
  </div>;
}

export function FavoritesView({ initialSession }: { initialSession: FavoritesRouteSession }) {
  const { status, favorites, loginHref, refresh, enterRoute } = useFavorites();
  const routeKey = initialSession.status === "authenticated" ? `user:${initialSession.userID}` : initialSession.status;
  const [enteredKey, setEnteredKey] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const focusedProduct = useRef<string | null>(null);
  const previousIDs = useRef<string[]>([]);
  useEffect(() => {
    enterRoute(initialSession);
    setEnteredKey(routeKey);
  }, [enterRoute, routeKey, initialSession]);

  // Before the route effect, render only its sanitized server gate. A warm
  // provider from another page must never flash its previous private list.
  const entered = enteredKey === routeKey;
  const displayStatus = entered ? status : initialSession.status === "authenticated" ? "loading" : initialSession.status;
  const visible = entered && displayStatus === "ready" ? favorites : null;
  useLayoutEffect(() => {
    if (!visible) return;
    const ids = visible.items.map((product) => product.id);
    const focused = focusedProduct.current;
    if (focused && !ids.includes(focused) && document.activeElement === document.body) {
      const index = Math.min(Math.max(previousIDs.current.indexOf(focused), 0), ids.length - 1);
      const nextCard = root.current?.querySelectorAll<HTMLElement>("[data-favorites-card]")[index];
      const nextControl = nextCard?.querySelector<HTMLElement>("[data-favorite-product]");
      focusedProduct.current = null;
      (nextControl ?? heading.current)?.focus();
    }
    previousIDs.current = ids;
  }, [visible]);

  return (
    <main className={styles.main} id="main-content">
      <Container>
        <div ref={root} onFocusCapture={(event) => {
          const card = (event.target as HTMLElement).closest<HTMLElement>("[data-favorites-card]");
          focusedProduct.current = card?.dataset.favoritesCard ?? null;
        }}>
          <header className={styles.heading}>
            <h1 ref={heading} tabIndex={-1}>Избранное</h1>
            {visible ? <p>Сохранено товаров: {visible.total}</p> : null}
          </header>
          {displayStatus === "anonymous" ? (
            <section className={styles.state}>
              <h2>Войдите, чтобы открыть избранное</h2>
              <p>Сохранённые товары доступны в вашем аккаунте. После входа вернитесь на эту страницу.</p>
              <div className={styles.actions}>
                {loginHref ? <a href={loginHref}>Войти</a> : null}
                <Button onClick={() => void refresh()} variant="secondary">Проверить вход</Button>
                <Link href="/catalog">Перейти в каталог</Link>
              </div>
            </section>
          ) : displayStatus === "unavailable" ? (
            <section className={styles.state}>
              <h2>Избранное временно недоступно</h2>
              <p>Не удалось проверить вход или загрузить сохранённые товары. Попробуйте ещё раз.</p>
              <Button onClick={() => void refresh()} variant="secondary">Повторить загрузку</Button>
            </section>
          ) : visible ? visible.items.length ? (
            <div className={styles.grid}>
              {visible.items.map((product) => (
                <div data-favorites-card={product.id} key={product.id}><ProductCard product={product} /></div>
              ))}
            </div>
          ) : (
            <section className={styles.state}>
              <h2>В избранном пока нет товаров</h2>
              <p>Сохраняйте понравившиеся товары с помощью сердечка.</p>
              <div className={styles.actions}><Link href="/catalog">Перейти в каталог</Link></div>
            </section>
          ) : <FavoritesSkeleton />}
        </div>
      </Container>
    </main>
  );
}
