import { notFound, redirect } from "next/navigation";

import { ProductDetail } from "@/components/storefront/product-detail";
import { getProduct, ProductApiError } from "@/lib/api/product";

type ProductPageProps = {
  params: Promise<{ slug: string }>;
};

export default async function ProductPage({ params }: ProductPageProps) {
  const { slug } = await params;

  try {
    const product = await getProduct(slug);
    if (slug !== product.slug) redirect(`/products/${product.slug}`);
    return <ProductDetail product={product} />;
  } catch (error) {
    if (error instanceof ProductApiError && error.kind === "not-found") notFound();
    throw error;
  }
}
