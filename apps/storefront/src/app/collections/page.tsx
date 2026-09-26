import type { Metadata } from "next";

import { CollectionsPage } from "@/components/storefront/collection-navigation";
import { getCollections } from "@/lib/api/collection";

export const metadata: Metadata = {
  title: "Подборки — Слой",
  description: "Редакционные подборки кондитерских изделий.",
};

export const dynamic = "force-dynamic";

export default async function Page() {
  const collections = await getCollections();
  return <CollectionsPage collections={collections} />;
}
