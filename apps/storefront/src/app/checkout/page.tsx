import type { Metadata } from "next";

import { CheckoutView } from "@/components/storefront/checkout-view";
import { getServerSession } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Оформление заказа — Слой", robots: { index: false, follow: false } };

// getServerSession() is only the route gate. Browser props receive status and
// user ID; the Cart, CSRF and cookies never reach server-rendered HTML.
export default async function CheckoutPage() {
  const session = await getServerSession();
  return <CheckoutView initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id }
    : { status: session.status }} />;
}
