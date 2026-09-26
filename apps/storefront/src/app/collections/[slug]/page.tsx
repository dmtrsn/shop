import type { Metadata } from "next";
import { notFound, redirect } from "next/navigation";

import { CollectionPage } from "@/components/storefront/collection-navigation";
import { getCollectionPageData } from "@/lib/api/collection";

type CollectionRouteProps = {
  params: Promise<{ slug: string }>;
};

export const metadata: Metadata = {
  title: "Подборка — Слой",
  description: "Редакционная подборка и связанные товары.",
};

export const dynamic = "force-dynamic";

export default async function Page({ params }: CollectionRouteProps) {
  const { slug } = await params;
  const data = await getCollectionPageData(slug);
  if (!data) notFound();
  if (slug !== data.collection.slug) {
    redirect(`/collections/${encodeURIComponent(data.collection.slug)}`);
  }
  return <CollectionPage data={data} />;
}
