"use client";

import { LegacyLoginLink } from "./legacy-login-link";

import { useEffect } from "react";

import { Button } from "@/components/ui/button";
import { HeartIcon } from "@/components/ui/icons";
import { IconButton } from "@/components/ui/icon-button";

import { useFavorites } from "./favorites-provider";
import styles from "./favorites.module.css";

export function FavoriteButton({ productID, productName, text = false }: { productID: string; productName: string; text?: boolean }) {
  const { status, favorites, mutationPending, pendingProductID, ensure, refresh, toggle, loginHref, signInRequired } = useFavorites();
  useEffect(() => { void ensure(); }, [ensure]);
  const selected = favorites?.items.some((product) => product.id === productID) ?? false;
  const className = `${styles.control} ${text ? styles.textControl : ""}`;
  if (status === "anonymous" && loginHref) {
    return (
      <LegacyLoginLink aria-label={`Войдите, чтобы сохранить: ${productName}`} className={className} href={loginHref} title="Войдите, чтобы сохранять избранное">
        <HeartIcon />{text ? <span>Войти, чтобы сохранить</span> : null}
      </LegacyLoginLink>
    );
  }
  const ready = status === "ready" && favorites !== null;
  const loading = status === "idle" || status === "loading";
  const label = ready ? `Избранное: ${productName}`
    : status === "anonymous" ? `Войдите, чтобы сохранить: ${productName}`
      : status === "unavailable" ? `Избранное недоступно. Повторить проверку: ${productName}`
        : `Проверяем избранное: ${productName}`;
  const props = {
    "aria-label": label,
    "aria-pressed": ready ? selected : undefined,
    "aria-disabled": loading || mutationPending,
    "aria-busy": loading || pendingProductID === productID,
    "data-favorite-product": productID,
    className,
    title: ready ? (selected ? "Удалить из избранного" : "Добавить в избранное") : label,
    onClick: () => {
      if (loading || mutationPending) return;
      if (ready) void toggle(productID);
      else if (status === "anonymous") signInRequired();
      else void refresh();
    },
    children: <><HeartIcon fill={ready && selected ? "currentColor" : "none"} />{text ? <span>{ready ? (selected ? "В избранном" : "В избранное") : status === "anonymous" ? "Для избранного нужно войти" : loading ? "Проверяем избранное…" : "Повторить проверку избранного"}</span> : null}</>,
  };
  return text ? <Button {...props} variant="secondary" /> : <IconButton {...props} />;
}
