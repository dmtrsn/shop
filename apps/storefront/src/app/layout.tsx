import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";

import { BrowserSessionProvider } from "@/components/storefront/browser-session-provider";
import { CartProvider } from "@/components/storefront/cart-provider";
import { FavoritesProvider } from "@/components/storefront/favorites-provider";
import { StorefrontHeader } from "@/components/storefront/header";
import { LegacyHashBridge } from "@/components/storefront/legacy-hash-bridge";
import { MobileTabBar } from "@/components/storefront/mobile-tab-bar";
import { SiteFooter } from "@/components/storefront/site-footer";

import "@/styles/globals.css";

export const metadata: Metadata = {
  title: "Слой — кондитерские изделия",
  description: "Слой — современный магазин шоколада, печенья, вафель и других кондитерских изделий.",
};

export const viewport: Viewport = {
  colorScheme: "light",
  // Lets the mobile tab bar sit above the home indicator via safe-area insets.
  viewportFit: "cover",
  // Browser chrome metadata cannot read CSS variables; mirrors --color-canvas.
  themeColor: "#f4f2ed",
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
        {/* Lazy private features share one in-memory browser session. The header,
            footer and tab bar stay outside them and never initialize Favorites or Cart. */}
        <BrowserSessionProvider>
          <FavoritesProvider>
            <CartProvider>{children}</CartProvider>
          </FavoritesProvider>
        </BrowserSessionProvider>
        <SiteFooter />
        <MobileTabBar />
      </body>
    </html>
  );
}
