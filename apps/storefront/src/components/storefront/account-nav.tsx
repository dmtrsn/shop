import Link from "next/link";

import { BagIcon, HeartIcon, ReceiptIcon, UserIcon } from "@/components/ui/icons";

import styles from "./account.module.css";

const sections = [
  { key: "account", href: "/account", label: "Аккаунт", Icon: UserIcon },
  { key: "orders", href: "/account/orders", label: "Мои заказы", Icon: ReceiptIcon },
  { key: "favorites", href: "/favorites", label: "Избранное", Icon: HeartIcon },
  { key: "cart", href: "/cart", label: "Корзина", Icon: BagIcon },
] as const;

// Plain links to existing routes. Rendering it never touches session, Cart or Favorites.
export function AccountNav({ current }: { current: "account" | "orders" }) {
  return (
    <nav aria-label="Разделы аккаунта" className={styles.nav}>
      {sections.map(({ key, href, label, Icon }) => (
        <Link aria-current={key === current ? "page" : undefined} href={href} key={key} prefetch={false}>
          <Icon aria-hidden="true" />{label}
        </Link>
      ))}
    </nav>
  );
}
