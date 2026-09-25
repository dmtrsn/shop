import Link from "next/link";

import { Container } from "@/components/ui/container";
import { CartIcon, HeartIcon, UserIcon } from "@/components/ui/icons";
import { SearchInput } from "@/components/ui/search-input";

import styles from "./header.module.css";

function SearchForm({ className }: { className?: string }) {
  return (
    <form action="/search" className={className} role="search">
      <SearchInput
        autoComplete="off"
        name="q"
        placeholder="Найти товар, артикул или производителя"
      />
    </form>
  );
}

export function StorefrontHeader() {
  return (
    <header className={styles.header}>
      <Container className={styles.desktop}>
        <Link className={styles.logo} href="/" aria-label="Слой — на главную">
          Слой
        </Link>
        <Link className={styles.navLink} href="/catalog">
          Каталог
        </Link>
        <Link className={styles.navLink} href="/manufacturers">
          Производители
        </Link>
        <Link className={styles.navLink} href="/collections">
          Подборки
        </Link>
        <SearchForm className={styles.search} />
        <Link className={styles.utilityLink} href="/favorites">
          <HeartIcon />
          <span className={styles.utilityLabel}>Избранное</span>
        </Link>
        <Link className={styles.utilityLink} href="/account">
          <UserIcon />
          <span className={styles.utilityLabel}>Аккаунт</span>
        </Link>
        <Link className={`${styles.utilityLink} ${styles.cart}`} href="/cart">
          <CartIcon />
          <span>Корзина</span>
        </Link>
      </Container>

      <Container className={styles.mobile}>
        <div className={styles.mobileTop}>
          <Link className={styles.logo} href="/" aria-label="Слой — на главную">
            Слой
          </Link>
          <Link className={styles.mobileCart} href="/cart">
            <CartIcon />
            <span>Корзина</span>
          </Link>
        </div>
        <SearchForm />
      </Container>
    </header>
  );
}
