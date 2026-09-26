import type { Metadata } from "next";
import { notFound, redirect } from "next/navigation";

import { ManufacturerPage } from "@/components/storefront/producer-navigation";
import { getManufacturerPageData } from "@/lib/api/producer";

type ManufacturerRouteProps = {
  params: Promise<{ slug: string }>;
};

export const metadata: Metadata = {
  title: "Производитель — Слой",
  description: "Бренды, серии и товары производителя.",
};

export const dynamic = "force-dynamic";

export default async function Page({ params }: ManufacturerRouteProps) {
  const { slug } = await params;
  const data = await getManufacturerPageData(slug);
  if (!data) notFound();
  if (slug !== data.manufacturer.slug) redirect(`/manufacturers/${encodeURIComponent(data.manufacturer.slug)}`);
  return <ManufacturerPage data={data} />;
}
