import type { Metadata } from "next";

import { OrdersView } from "@/components/storefront/orders-view";
import { getServerSession } from "@/lib/api/session";
import { getLegacyLoginHref } from "@/lib/legacy-login";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Мои заказы — Слой", robots: { index: false, follow: false } };

// Only the sanitized session gate crosses this boundary. History is loaded in
// the browser with owner binding, never embedded in server HTML or cached.
export default async function OrdersPage() {
  const session = await getServerSession();
  return <OrdersView initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id }
    : { status: session.status }} loginHref={getLegacyLoginHref()} />;
}
