import type { Metadata } from "next";

import { CategoriesPage } from "@/components/storefront/category-navigation";
import { getCategories } from "@/lib/api/category";

export const metadata: Metadata = {
  title: "Категории — Слой",
  description: "Справочник категорий кондитерских изделий.",
};

export const dynamic = "force-dynamic";

export default async function Page() {
  const categories = await getCategories();
  return <CategoriesPage categories={categories} />;
}
