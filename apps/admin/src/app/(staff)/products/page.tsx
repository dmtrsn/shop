import type { Metadata } from "next";

import { ProductList } from "@/components/product-list";
import { parseProductQuery } from "@/lib/product-query";

export const metadata: Metadata = { title: "Товары" };

type Props = { searchParams: Promise<Record<string, string | string[] | undefined>> };

// The (staff) layout has already confirmed the session on the server; the
// list itself loads in the browser, so no product data is cached server-side.
export default async function ProductsPage({ searchParams }: Props) {
  return <ProductList query={parseProductQuery(await searchParams)} />;
}
