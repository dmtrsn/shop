import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";

import { StorefrontHeader } from "@/components/storefront/header";
import { FavoritesProvider } from "@/components/storefront/favorites-provider";
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
  return (
    <html lang="ru">
      <body>
        <a className="skip-link" href="#main-content">
          Перейти к содержимому
        </a>
        <StorefrontHeader />
        <FavoritesProvider loginHref={getLegacyLoginHref()}>{children}</FavoritesProvider>
      </body>
    </html>
  );
}
