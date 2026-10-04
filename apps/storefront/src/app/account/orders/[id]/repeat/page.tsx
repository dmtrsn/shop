import type { Metadata } from "next";
import { RepeatView } from "@/components/storefront/repeat-view";
import { routeOrderID } from "@/lib/api/order-client";
import { getServerSession } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Повторить заказ — Слой", robots: { index: false, follow: false } };
export default async function RepeatPage({ params }: { params: Promise<{ id: string }> }) {
  const [{ id }, session] = await Promise.all([params, getServerSession()]);
  return <RepeatView orderID={routeOrderID(id)} initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id } : { status: session.status }} />;
}
