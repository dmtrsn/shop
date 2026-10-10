import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";

import { BrowserSessionProvider } from "@/components/storefront/browser-session-provider";
import { CartProvider } from "@/components/storefront/cart-provider";
import { FavoritesProvider } from "@/components/storefront/favorites-provider";
import { StorefrontHeader } from "@/components/storefront/header";
import { LegacyHashBridge } from "@/components/storefront/legacy-hash-bridge";

import "@/styles/globals.css";

export const metadata: Metadata = {
  title: "Слой — кондитерские изделия",
  description: "Слой — современный магазин шоколада, печенья, вафель и других кондитерских изделий.",
};

export const viewport: Viewport = {
  colorScheme: "light",
  themeColor: "#f6f2e9",
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="ru">
      <body>
        <LegacyHashBridge />
        <a className="skip-link" href="#main-content">
          Перейти к содержимому
        </a>
        <StorefrontHeader />
        {/* Lazy private features share one in-memory browser session. The header
            stays outside them and never initializes Favorites or Cart. */}
        <BrowserSessionProvider>
          <FavoritesProvider>
            <CartProvider>{children}</CartProvider>
          </FavoritesProvider>
        </BrowserSessionProvider>
      </body>
    </html>
  );
}
