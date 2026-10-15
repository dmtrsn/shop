"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { BagIcon, GridIcon, HeartIcon, HomeIcon, UserIcon } from "@/components/ui/icons";

import styles from "./mobile-tab-bar.module.css";

// Discovery routes that belong to the Каталог tab.
const catalogSections = ["/catalog", "/categories", "/collections", "/manufacturers", "/products", "/search"];

const tabs = [
  { href: "/", label: "Главная", Icon: HomeIcon, prefetch: undefined, match: (path: string) => path === "/" },
  { href: "/catalog", label: "Каталог", Icon: GridIcon, prefetch: undefined,
    match: (path: string) => catalogSections.some((section) => path === section || path.startsWith(`${section}/`)) },
  { href: "/favorites", label: "Избранное", Icon: HeartIcon, prefetch: false, match: (path: string) => path === "/favorites" },
  { href: "/cart", label: "Корзина", Icon: BagIcon, prefetch: false,
    match: (path: string) => path === "/cart" || path === "/checkout" },
  { href: "/account", label: "Аккаунт", Icon: UserIcon, prefetch: false,
    match: (path: string) => path === "/account" || path.startsWith("/account/") || path === "/login" },
] as const;

// Fixed mobile navigation (shown at ≤900px by CSS). Plain links only: it reads
// the pathname for the current tab and never touches the session, Cart or
// Favorites, so it shows no counts.
export function MobileTabBar() {
  const pathname = usePathname() ?? "";
  return (
    <nav aria-label="Основная навигация" className={styles.bar}>
      {tabs.map(({ href, label, Icon, prefetch, match }) => {
        const current = match(pathname);
        return (
          <Link
            aria-current={current ? "page" : undefined}
            className={styles.tab}
            href={href}
            key={href}
            prefetch={prefetch}
          >
            <Icon />
            <span>{label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
