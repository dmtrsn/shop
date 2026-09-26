import type { Metadata } from "next";
import { notFound, redirect } from "next/navigation";

import { CategoryPage } from "@/components/storefront/category-navigation";
import { getCategoryPageData } from "@/lib/api/category";
import { categoryUrl } from "@/lib/category-tree";

type CategoryRouteProps = {
  params: Promise<{ slug: string }>;
};

export const metadata: Metadata = {
  title: "Категория — Слой",
  description: "Подкатегории и товары категории.",
};

export const dynamic = "force-dynamic";

export default async function Page({ params }: CategoryRouteProps) {
  const { slug } = await params;
  const data = await getCategoryPageData(slug);
  if (!data) notFound();
  if (slug !== data.category.slug) redirect(categoryUrl(data.category.slug));
  return <CategoryPage data={data} />;
}
