import Link from "next/link";

import type { Product } from "@/lib/api/product";

import { CatalogProductImage } from "./catalog-product-image";
import { FavoriteButton } from "./favorite-button";
import styles from "./catalog.module.css";

function formatMoney(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat("ru-RU", {
    currency,
    style: "currency",
  }).format(amountMinor / 100);
}

function stockLabel(product: Product): string {
  if (product.stock <= 0) return "Нет в наличии";
  if (product.stock < product.minimumQuantity) return "Меньше минимального количества";
  return "В наличии";
}

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
      {product.badges.length ? (
        <span className={styles.badges}>
          {product.badges.slice(0, 2).map((badge) => (
            <span key={badge}>{badge}</span>
          ))}
        </span>
      ) : null}
    </Link>
  );
}

function ProductPrice({ product }: { product: Product }) {
  const hasOldPrice = Boolean(
    product.oldPriceMinor && product.oldPriceMinor > product.priceMinor,
  );
  return (
    <p aria-label="Цена" className={styles.price}>
      <strong>{formatMoney(product.priceMinor, product.currency)}</strong>
      {hasOldPrice ? (
        <del>{formatMoney(product.oldPriceMinor as number, product.currency)}</del>
      ) : null}
    </p>
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
        <p className={styles.productMeta}>
          <span>Арт. {product.sku}</span>
          <span>{product.unit}</span>
        </p>
        <p className={product.stock >= product.minimumQuantity ? styles.inStock : styles.outOfStock}>
          <span aria-hidden="true" />
          {stockLabel(product)}
        </p>
        <div className={styles.cardFooter}>
          <ProductPrice product={product} />
          <Link className={styles.productLink} href={`/products/${product.slug}`}>
            Подробнее
          </Link>
        </div>
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
      <p className={product.stock >= product.minimumQuantity ? styles.inStock : styles.outOfStock}>
        <span aria-hidden="true" />
        {stockLabel(product)}
      </p>
      <ProductPrice product={product} />
      <div className={styles.compactOpen}>
        <FavoriteButton productID={product.id} productName={product.name} />
        <Link href={`/products/${product.slug}`}>Открыть</Link>
      </div>
    </article>
  );
}
