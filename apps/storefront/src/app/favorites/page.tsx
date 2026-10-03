import type { Metadata } from "next";

import { FavoritesView } from "@/components/storefront/favorites-view";
import { getServerSession } from "@/lib/api/session";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Избранное — Слой", robots: { index: false, follow: false } };

export default async function FavoritesPage() {
  const session = await getServerSession();
  return <FavoritesView initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id }
    : { status: session.status }} />;
}
