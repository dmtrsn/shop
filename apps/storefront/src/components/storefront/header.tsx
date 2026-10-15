import Link from "next/link";

import { ButtonLink } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { BagIcon, GridIcon, HeartIcon, ReceiptIcon, UserIcon } from "@/components/ui/icons";
import { LogoMark } from "@/components/ui/logo-mark";

import { GlobalSearch } from "./global-search";
import styles from "./header.module.css";

// Public chrome only: plain links and the search combobox. It renders outside
// the private providers and never reads the session, Cart or Favorites, so it
// shows no counts. On mobile the bottom tab bar carries the customer actions.
const actions = [
  { href: "/favorites", label: "Избранное", Icon: HeartIcon },
  { href: "/account/orders", label: "Заказы", Icon: ReceiptIcon },
  { href: "/account", label: "Аккаунт", Icon: UserIcon },
  { href: "/cart", label: "Корзина", Icon: BagIcon },
];

export function StorefrontHeader() {
  return (
    <header className={styles.header}>
      <Container className={styles.bar}>
        <Link aria-label="Слой — на главную" className={styles.logo} href="/">
          <LogoMark wordmark />
        </Link>
        <ButtonLink className={styles.catalog} href="/catalog">
          <GridIcon />
          Каталог
        </ButtonLink>
        <GlobalSearch className={styles.search} />
        <nav aria-label="Покупки и аккаунт" className={styles.actions}>
          {actions.map(({ href, label, Icon }) => (
            // Private destinations are never prefetched.
            <Link className={styles.action} href={href} key={href} prefetch={false}>
              <Icon />
              <span className={styles.actionLabel}>{label}</span>
            </Link>
          ))}
        </nav>
      </Container>
    </header>
  );
}
