import Link from "next/link";

import type { Product } from "@/lib/api/product";

import { CartControl } from "./cart-control";
import { CatalogProductImage } from "./catalog-product-image";
import { FavoriteButton } from "./favorite-button";
import { ProductBadges } from "./product-badges";
import { ProductOffer } from "./product-offer";
import styles from "./catalog.module.css";

function productLineage(product: Product): string {
  const values = [product.manufacturer.name];
  if (product.brand.name && product.brand.name !== product.manufacturer.name) {
    values.push(product.brand.name);
  }
  if (product.series) values.push(product.series);
  return values.join(" · ");
}

function ProductMedia({ product }: { product: Product }) {
  return (
    <Link
      aria-label={`Открыть товар: ${product.name}`}
      className={styles.productMedia}
      href={`/products/${product.slug}`}
    >
      <CatalogProductImage image={product.image} name={product.name} />
      <ProductBadges badges={product.badges} className={styles.badges} />
    </Link>
  );
}

export function ProductCard({
  headingLevel = 2,
  product,
}: {
  headingLevel?: 2 | 3;
  product: Product;
}) {
  const title = (
    <Link href={`/products/${product.slug}`}>{product.name}</Link>
  );
  return (
    <article className={styles.productCard}>
      <div className={styles.cardMedia}>
        <ProductMedia product={product} />
        <div className={styles.favoritePosition}><FavoriteButton productID={product.id} productName={product.name} /></div>
      </div>
      <div className={styles.cardBody}>
        <p className={styles.lineage}>{productLineage(product)}</p>
        {headingLevel === 3 ? (
          <h3 className={styles.productName}>{title}</h3>
        ) : (
          <h2 className={styles.productName}>{title}</h2>
        )}
        {/* Unit and SKU stay as an accessible secondary line (UX spec); the
            image and title are the product links, so no separate "Подробнее". */}
        <p className={styles.productMeta}>
          <span>{product.unit}</span>
          <span>Арт. {product.sku}</span>
        </p>
        <div className={styles.cardFooter}>
          <ProductOffer product={product} part="price" />
          <ProductOffer product={product} part="stock" />
        </div>
        <div className={styles.cardCart}><CartControl product={product} /></div>
      </div>
    </article>
  );
}

export function CompactProductCard({ product }: { product: Product }) {
  return (
    <article className={styles.compactProduct}>
      <div className={styles.compactImage}>
        <ProductMedia product={product} />
      </div>
      <div className={styles.compactIdentity}>
        <h2>
          <Link href={`/products/${product.slug}`}>{product.name}</Link>
        </h2>
        <span>Арт. {product.sku}</span>
      </div>
      <p className={styles.compactLineage}>{productLineage(product)}</p>
      <p className={styles.compactUnit}>{product.unit}</p>
      <ProductOffer product={product} part="stock" />
      <ProductOffer product={product} part="price" />
      <div className={styles.compactCart}><CartControl compact product={product} /></div>
      <div className={styles.compactOpen}>
        <FavoriteButton productID={product.id} productName={product.name} />
        <Link href={`/products/${product.slug}`}>Открыть</Link>
      </div>
    </article>
  );
}
