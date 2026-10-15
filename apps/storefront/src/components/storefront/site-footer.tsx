import Link from "next/link";

import { Container } from "@/components/ui/container";
import { LogoMark } from "@/components/ui/logo-mark";

import styles from "./site-footer.module.css";

// Only real storefront routes. No contact, delivery, legal or VAT claims: the
// shop publishes none. It reads no session, so the account entry is neutral.
const columns = [
  {
    title: "Каталог",
    links: [
      { href: "/catalog", label: "Все товары" },
      { href: "/categories", label: "Категории" },
      { href: "/manufacturers", label: "Производители" },
      { href: "/collections", label: "Подборки" },
    ],
  },
  {
    title: "Покупателям",
    links: [
      { href: "/favorites", label: "Избранное", private: true },
      { href: "/cart", label: "Корзина", private: true },
      { href: "/account/orders", label: "Мои заказы", private: true },
      { href: "/account", label: "Аккаунт", private: true },
    ],
  },
];

export function SiteFooter() {
  return (
    <footer className={styles.footer}>
      <Container className={styles.grid}>
        <div className={styles.brand}>
          <Link aria-label="Слой — на главную" className={styles.logo} href="/">
            <LogoMark tone="inverse" wordmark />
          </Link>
          <p>
            Магазин кондитерских изделий: шоколад, конфеты, печенье, пастила, мармелад и подарочные
            наборы от разных производителей.
          </p>
        </div>
        {columns.map((column) => (
          <nav aria-label={column.title} className={styles.column} key={column.title}>
            <h2>{column.title}</h2>
            <ul>
              {column.links.map((link) => (
                <li key={link.href}>
                  <Link href={link.href} prefetch={"private" in link ? false : undefined}>{link.label}</Link>
                </li>
              ))}
            </ul>
          </nav>
        ))}
      </Container>
      <Container className={styles.bottom}>
        <span>© Слой</span>
      </Container>
    </footer>
  );
}
