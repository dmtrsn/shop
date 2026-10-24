import type { Metadata } from "next";

import { ProductEditor } from "@/components/product-editor";

export const metadata: Metadata = { title: "Товар" };

type Props = {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

// Keyed by ID, so moving to another product starts a fresh editor.
export default async function ProductPage({ params, searchParams }: Props) {
  const { id } = await params;
  const created = (await searchParams).created === "1";
  return <ProductEditor created={created} key={id} productId={id} />;
}
