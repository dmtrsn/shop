import type { Metadata } from "next";

import { ProductEditor } from "@/components/product-editor";

export const metadata: Metadata = { title: "Новый товар" };

export default function NewProductPage() {
  return <ProductEditor />;
}
