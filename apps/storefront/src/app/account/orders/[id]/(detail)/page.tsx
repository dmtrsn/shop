import type { Metadata } from "next";

import { OrderView } from "@/components/storefront/order-view";
import { routeOrderID } from "@/lib/api/order-client";
import { getServerSession } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Заказ — Слой", robots: { index: false, follow: false } };

type OrderPageProps = { params: Promise<{ id: string }> };

// The server only gates the route. The order itself is read in the browser,
// bound to the current session user, and never rendered into server HTML.
export default async function OrderPage({ params }: OrderPageProps) {
  const [{ id }, session] = await Promise.all([params, getServerSession()]);
  return <OrderView initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id }
    : { status: session.status }} orderID={routeOrderID(id)} />;
}
