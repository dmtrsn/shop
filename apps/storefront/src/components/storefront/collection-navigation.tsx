import Link from "next/link";

import { Container } from "@/components/ui/container";
import type { Collection, CollectionPageData } from "@/lib/api/collection";

import { CatalogProductImage } from "./catalog-product-image";
import { ProductCard } from "./product-card";
import styles from "./collection-navigation.module.css";

function productCountText(count: number): string {
  const modulo100 = count % 100;
  const modulo10 = count % 10;
  const noun = modulo100 >= 11 && modulo100 <= 14
    ? "товаров"
    : modulo10 === 1
      ? "товар"
      : modulo10 >= 2 && modulo10 <= 4
        ? "товара"
        : "товаров";
  return `${count} ${noun}`;
}

function collectionUrl(collection: Pick<Collection, "slug">): string {
  return `/collections/${encodeURIComponent(collection.slug)}`;
}

function CollectionImage({ collection }: { collection: Collection }) {
  return (
    <CatalogProductImage
      image={collection.image ?? ""}
      name={`Подборка «${collection.name}»`}
    />
  );
}

export function CollectionsPage({ collections }: { collections: Collection[] }) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page">Подборки</span>
        </nav>

        <header className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Редакционный выбор</p>
          <h1>Подборки</h1>
          <p>
            Независимый способ открыть товары по теме или случаю — без подмены категорий и производителей.
          </p>
        </header>

        {collections.length ? (
          <ol className={styles.collectionGrid}>
            {collections.map((collection) => (
              <li key={collection.id}>
                <article className={styles.collectionCard}>
                  <Link
                    aria-label={`Открыть подборку: ${collection.name}`}
                    className={styles.collectionImage}
                    href={collectionUrl(collection)}
                  >
                    <CollectionImage collection={collection} />
                  </Link>
                  <div className={styles.collectionCopy}>
                    <p className={styles.count}>{productCountText(collection.productCount)}</p>
                    <h2><Link href={collectionUrl(collection)}>{collection.name}</Link></h2>
                    {collection.description ? <p>{collection.description}</p> : null}
                    <Link className={styles.textLink} href={collectionUrl(collection)}>
                      Смотреть подборку <span aria-hidden="true">→</span>
                    </Link>
                  </div>
                </article>
              </li>
            ))}
          </ol>
        ) : (
          <div className={styles.state}>
            <h2>Подборок пока нет</h2>
            <p>Редакционные подборки ещё не опубликованы. Все доступные товары остаются в каталоге.</p>
            <Link href="/catalog">Открыть каталог</Link>
          </div>
        )}
      </Container>
    </main>
  );
}

export function CollectionPage({ data }: { data: CollectionPageData }) {
  const { collection, products } = data;
  const catalogUrl = `/catalog?collection=${encodeURIComponent(collection.slug)}`;

  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <Link href="/collections">Подборки</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page">{collection.name}</span>
        </nav>

        <header className={styles.detailHeading}>
          <div className={styles.detailCopy}>
            <p className={styles.eyebrow}>Подборка</p>
            <h1>{collection.name}</h1>
            {collection.description ? <p>{collection.description}</p> : null}
            <p className={styles.detailCount}>{productCountText(collection.productCount)}</p>
            <Link className={styles.primaryLink} href={catalogUrl}>
              Все товары подборки
            </Link>
          </div>
          <div className={styles.detailImage}>
            <CollectionImage collection={collection} />
          </div>
        </header>

        <section aria-labelledby="collection-products" className={styles.productsSection}>
          {products.length ? (
            <>
              <div className={styles.sectionHeading}>
                <div>
                  <p className={styles.eyebrow}>В редакционном порядке</p>
                  <h2 id="collection-products">Товары в подборке</h2>
                </div>
                <Link href={catalogUrl}>Открыть в каталоге →</Link>
              </div>
              <ol className={styles.productGrid}>
                {products.map((product) => (
                  <li key={product.id}>
                    <ProductCard headingLevel={3} product={product} />
                  </li>
                ))}
              </ol>
            </>
          ) : (
            <div className={styles.state}>
              <h2 id="collection-products">В подборке пока нет товаров</h2>
              <p>Сама подборка доступна, но связанные товары ещё не добавлены.</p>
              <Link href="/catalog">Открыть весь каталог</Link>
            </div>
          )}
        </section>
      </Container>
    </main>
  );
}
