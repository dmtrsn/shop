import type { Metadata } from "next";

import { HomePage } from "@/components/storefront/home-page";
import { getHomePageData } from "@/lib/api/home";

export const metadata: Metadata = {
  title: "Слой — кондитерские изделия",
  description: "Слой — магазин шоколада, конфет, печенья, пастилы и других кондитерских изделий.",
};

// Go applies collection schedules per request, so the page is never prerendered.
export const dynamic = "force-dynamic";

export default async function Page() {
  const data = await getHomePageData();
  return <HomePage data={data} />;
}
