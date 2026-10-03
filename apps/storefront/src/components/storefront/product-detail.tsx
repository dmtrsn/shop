import Link from "next/link";
import type { ReactNode } from "react";

import { Container } from "@/components/ui/container";
import type { Product } from "@/lib/api/product";
import { categoryUrl } from "@/lib/category-tree";

import { ProductGallery } from "./product-gallery";
import { ProductOffer } from "./product-offer";
import { ProductPurchase } from "./product-purchase";
import styles from "./product-detail.module.css";

type ProductDetailProps = {
  product: Product;
};

function productImages(product: Product): string[] {
  return Array.from(new Set([product.image, ...product.gallery].filter(Boolean)));
}

export function ProductDetail({ product }: ProductDetailProps) {
  const attributes: Array<[string, ReactNode] | undefined> = [
    ["Категория", <Link href={categoryUrl(product.category.slug)} key="category">{product.category.name}</Link>],
    ["Производитель", product.manufacturer.name],
    ["Бренд", product.brand.name],
    product.series ? ["Серия", product.series] : undefined,
    product.ingredients ? ["Состав", product.ingredients] : undefined,
    product.nutrition ? ["Пищевая ценность", product.nutrition] : undefined,
    product.shelfLifeDays ? ["Срок хранения", `${product.shelfLifeDays} дней`] : undefined,
    product.tags.length ? ["Характеристики", product.tags.join(", ")] : undefined,
  ];
  const presentAttributes = attributes.filter(
    (entry): entry is [string, ReactNode] => entry !== undefined,
  );

  return (
    <main className={styles.main} id="main-content">
      <Container>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page">{product.name}</span>
        </nav>

        <article>
          <div className={styles.productGrid}>
            <ProductGallery images={productImages(product)} productName={product.name} />

            <div className={styles.productInfo}>
              <p aria-label="Производитель, бренд и серия" className={styles.lineage}>
                <span>{product.manufacturer.name}</span>
                <span aria-hidden="true">/</span>
                <span>{product.brand.name}</span>
                {product.series ? (
                  <>
                    <span aria-hidden="true">/</span>
                    <span>{product.series}</span>
                  </>
                ) : null}
              </p>

              <h1>{product.name}</h1>
              <div className={styles.identity}>
                <span>Артикул: {product.sku}</span>
                <span>{product.unit}</span>
                {product.weightGrams && !product.unit.includes(String(product.weightGrams)) ? (
                  <span>{product.weightGrams} г</span>
                ) : null}
              </div>

              <ProductOffer product={product} part="price" detail />
              <ProductOffer product={product} part="stock" detail />

              <ProductPurchase product={product} />
            </div>
          </div>

          <div className={styles.details}>
            {product.description ? (
              <section aria-labelledby="description-heading" className={styles.description}>
                <h2 id="description-heading">Описание</h2>
                <p>{product.description}</p>
              </section>
            ) : null}

            {presentAttributes.length ? (
              <section aria-labelledby="attributes-heading" className={styles.attributes}>
                <h2 id="attributes-heading">О товаре</h2>
                <dl>
                  {presentAttributes.map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{value}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            ) : null}
          </div>
        </article>
      </Container>
    </main>
  );
}
