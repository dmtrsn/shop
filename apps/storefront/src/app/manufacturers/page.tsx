import type { Metadata } from "next";

import { ManufacturersPage } from "@/components/storefront/producer-navigation";
import { getManufacturers } from "@/lib/api/producer";

export const metadata: Metadata = {
  title: "Производители — Слой",
  description: "Алфавитный справочник производителей кондитерских изделий.",
};

export const dynamic = "force-dynamic";

export default async function Page() {
  const manufacturers = await getManufacturers();
  return <ManufacturersPage manufacturers={manufacturers} />;
}
