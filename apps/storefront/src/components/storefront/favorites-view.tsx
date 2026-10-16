"use client";

import { LoginLink } from "./login-link";

import Link from "next/link";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

import { Button, ButtonLink, buttonClassName } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { EmptyState } from "@/components/ui/empty-state";
import { HeartIcon, InfoIcon } from "@/components/ui/icons";
import { PageHeader } from "@/components/ui/page-header";

import { useFavorites, type FavoritesRouteSession } from "./favorites-provider";
import { ProductCard } from "./product-card";
import styles from "./favorites.module.css";

export function FavoritesSkeleton() {
  return <div aria-busy="true" aria-label="Загружаем избранное" className={styles.grid}>
    {Array.from({ length: 4 }, (_, index) => <div aria-hidden="true" className={styles.skeleton} key={index} />)}
  </div>;
}

export function FavoritesView({ initialSession }: { initialSession: FavoritesRouteSession }) {
  const { status, favorites, refresh, enterRoute } = useFavorites();
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
          <PageHeader
            subtitle={visible ? `Сохранено товаров: ${visible.total}` : undefined}
            title="Избранное"
            titleProps={{ ref: heading, tabIndex: -1 }}
          />
          {displayStatus === "anonymous" ? (
            <EmptyState
              actions={<>
                <LoginLink className={buttonClassName("primary")}>Войти</LoginLink>
                <Button onClick={() => void refresh()} variant="secondary">Проверить вход</Button>
                <Link className={buttonClassName("tertiary")} href="/catalog">Перейти в каталог</Link>
              </>}
              icon={<HeartIcon />}
              title="Войдите, чтобы открыть избранное"
            >
              Сохранённые товары доступны в вашем аккаунте. После входа вернитесь на эту страницу.
            </EmptyState>
          ) : displayStatus === "unavailable" ? (
            // A failure, not an empty list: a solid notice instead of the dashed empty state.
            <section className={styles.unavailable}>
              <InfoIcon aria-hidden="true" />
              <div>
                <h2>Избранное временно недоступно</h2>
                <p>Не удалось проверить вход или загрузить сохранённые товары. Попробуйте ещё раз.</p>
                <Button onClick={() => void refresh()} variant="secondary">Повторить загрузку</Button>
              </div>
            </section>
          ) : visible ? visible.items.length ? (
            <div className={styles.grid}>
              {visible.items.map((product) => (
                <div data-favorites-card={product.id} key={product.id}><ProductCard product={product} /></div>
              ))}
            </div>
          ) : (
            <EmptyState
              actions={<ButtonLink href="/catalog">Перейти в каталог</ButtonLink>}
              icon={<HeartIcon />}
              title="В избранном пока нет товаров"
            >
              Сохраняйте понравившиеся товары с помощью сердечка.
            </EmptyState>
          ) : <FavoritesSkeleton />}
        </div>
      </Container>
    </main>
  );
}
