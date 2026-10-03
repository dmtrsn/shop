import type { Metadata } from "next";

import { CartView } from "@/components/storefront/cart-view";
import { getServerSession } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Корзина — Слой", robots: { index: false, follow: false } };

// getServerSession() is only the route gate. Browser props receive status and
// user ID; CSRF, cookies and raw session data stay on the server.
export default async function CartPage() {
  const session = await getServerSession();
  return <CartView initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id }
    : { status: session.status }} />;
}
