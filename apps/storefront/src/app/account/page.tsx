import type { Metadata } from "next";

import { AccountView } from "@/components/storefront/account-view";
import { getServerSession } from "@/lib/api/session";
import { getLegacyLoginHref } from "@/lib/legacy-login";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "Аккаунт — Слой", robots: { index: false, follow: false } };

export default async function AccountPage() {
  const session = await getServerSession();
  return <AccountView initialSession={session.status === "authenticated"
    ? { status: "authenticated", userID: session.user.id }
    : { status: session.status }} loginHref={getLegacyLoginHref()} />;
}
