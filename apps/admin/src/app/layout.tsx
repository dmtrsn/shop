import type { Metadata, Viewport } from "next";
import { connection } from "next/server";
import type { ReactNode } from "react";

import "@/styles/globals.css";

export const metadata: Metadata = {
  title: { default: "Администрирование — Слой", template: "%s — Администрирование Слой" },
  robots: { index: false, follow: false },
};

export const viewport: Viewport = {
  colorScheme: "light",
  // Browser chrome metadata cannot read CSS variables; mirrors --color-canvas.
  themeColor: "#f4f2ed",
};

// Every admin page, including the 404, renders per request: the CSP nonce
// exists only at request time, and no staff page may be prerendered or cached.
export default async function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  await connection();
  return (
    <html lang="ru">
      <body>{children}</body>
    </html>
  );
}
