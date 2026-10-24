"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useRef, useState, type ReactNode } from "react";

import { useStaffSession } from "./staff-session";
import styles from "./admin-shell.module.css";

const sections = [
  { href: "/", label: "Обзор", current: (path: string) => path === "/" },
  { href: "/products", label: "Товары", current: (path: string) => path === "/products" || path.startsWith("/products/") },
  { href: "/orders", label: "Заказы", current: (path: string) => path === "/orders" || path.startsWith("/orders/") },
];

export function AdminShell({ children }: { children: ReactNode }) {
  const { staff, signOut } = useStaffSession();
  const pathname = usePathname();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const messageRef = useRef<HTMLParagraphElement>(null);

  async function handleSignOut() {
    setBusy(true);
    setMessage(null);
    const result = await signOut();
    setBusy(false);
    if (result) {
      setMessage(result);
      requestAnimationFrame(() => messageRef.current?.focus());
    }
  }

  return (
    <div className={styles.shell}>
      <a className={styles.skipLink} href="#main-content">
        Перейти к содержимому
      </a>
      <header className={styles.header}>
        <p className={styles.brand}>
          Слой <span>Администрирование</span>
        </p>
        <nav aria-label="Разделы" className={styles.nav}>
          <ul>
            {sections.map((section) => (
              <li key={section.href}>
                <Link aria-current={section.current(pathname) ? "page" : undefined} href={section.href}>
                  {section.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <div className={styles.identity}>
          <p>
            <span className={styles.staffName}>{staff.displayName}</span>
            <span className={styles.staffEmail}>{staff.email}</span>
          </p>
          <button aria-busy={busy} className={styles.signOut} disabled={busy} onClick={handleSignOut} type="button">
            {busy ? "Выходим…" : "Выйти"}
          </button>
        </div>
      </header>
      {message ? (
        <p className={styles.alert} ref={messageRef} role="alert" tabIndex={-1}>
          {message}
        </p>
      ) : null}
      <main className={styles.main} id="main-content" tabIndex={-1}>
        {children}
      </main>
    </div>
  );
}
