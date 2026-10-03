import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";

import { BrowserSessionProvider } from "@/components/storefront/browser-session-provider";
import { CartProvider } from "@/components/storefront/cart-provider";
import { FavoritesProvider } from "@/components/storefront/favorites-provider";
import { StorefrontHeader } from "@/components/storefront/header";
import { getLegacyLoginHref } from "@/lib/legacy-login";

import "@/styles/globals.css";

export const metadata: Metadata = {
  title: "Слой — новая витрина",
  description: "Изолированная основа будущей витрины магазина кондитерских изделий.",
};

export const viewport: Viewport = {
  colorScheme: "light",
  themeColor: "#f6f2e9",
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  const loginHref = getLegacyLoginHref();
  return (
    <html lang="ru">
      <body>
        <a className="skip-link" href="#main-content">
          Перейти к содержимому
        </a>
        <StorefrontHeader />
        {/* Lazy private features share one in-memory browser session. The header
            stays outside them and never initializes Favorites or Cart. */}
        <BrowserSessionProvider>
          <FavoritesProvider loginHref={loginHref}>
            <CartProvider loginHref={loginHref}>{children}</CartProvider>
          </FavoritesProvider>
        </BrowserSessionProvider>
      </body>
    </html>
  );
}
