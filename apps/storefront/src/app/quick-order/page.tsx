import type { Metadata } from "next";
import { Suspense } from "react";

import { QuickOrderView } from "@/components/storefront/quick-order-view";
import { getManufacturers } from "@/lib/api/producer";

import Loading from "./loading";

export const metadata: Metadata = {
  title: "Быстрый заказ — Слой",
  description: "Поиск товаров по производителям",
};
export const dynamic = "force-dynamic";

export default async function Page() {
  const manufacturers = await getManufacturers();
  return <Suspense fallback={<Loading />}><QuickOrderView manufacturers={manufacturers} /></Suspense>;
}
