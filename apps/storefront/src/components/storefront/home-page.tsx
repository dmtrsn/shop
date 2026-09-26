import Link from "next/link";

import { Container } from "@/components/ui/container";
import type {
  HomeCategories,
  HomeCollection,
  HomeManufacturers,
  HomePageData,
} from "@/lib/api/home";
import { categoryUrl } from "@/lib/category-tree";
import { productCountText } from "@/lib/plural";

import { CatalogProductImage } from "./catalog-product-image";
import { categoryCountText } from "./category-navigation";
import { collectionUrl } from "./collection-navigation";
import { ProductCard } from "./product-card";
import styles from "./home-page.module.css";

export const HOME_TITLE = "Кондитерские изделия";

function manufacturerUrl(slug: string): string {
  return `/manufacturers/${encodeURIComponent(slug)}`;
}

export function HomeHero({ collection }: { collection?: HomeCollection["collection"] }) {
  return (
    <section aria-labelledby="home-title" className={styles.hero}>
      <Container className={styles.heroInner}>
        <div className={styles.heroCopy}>
          <h1 className={styles.title} id="home-title">{HOME_TITLE}</h1>
          <p className={styles.lede}>
            Шоколад, конфеты, печенье и пастила. Ищите по названию, артикулу, производителю или
            бренду — или начните с категории.
          </p>
          <div className={styles.heroActions}>
            <Link className={styles.primaryLink} href="/catalog">
              Открыть каталог
            </Link>
            {collection ? (
              <Link className={styles.textLink} href={collectionUrl(collection)}>
                Подборка: {collection.name} <span aria-hidden="true">→</span>
              </Link>
            ) : null}
          </div>
        </div>
        <div className={styles.heroMedia}>
          {/* Decorative confectionery photograph served by the legacy Go assets. */}
          <img
            alt=""
            className={styles.heroImage}
            decoding="async"
            fetchPriority="high"
            height={941}
            src="/assets/hero-confectionery.webp"
            width={1672}
          />
        </div>
      </Container>
    </section>
  );
}

function CategoriesSection({ data }: { data: HomeCategories }) {
  return (
    <section aria-labelledby="home-categories" className={styles.section}>
      <Container>
        <div className={styles.sectionHeader}>
          <h2 id="home-categories">Категории</h2>
          <Link className={styles.textLink} href="/categories">
            {data.totalRoots > data.categories.length
              ? `Все категории (${data.totalRoots})`
              : "Все категории"}{" "}
            <span aria-hidden="true">→</span>
          </Link>
        </div>
        <ul className={styles.categoryList}>
          {data.categories.map((category) => (
            <li className={styles.categoryItem} key={category.id}>
              <Link className={styles.itemName} href={categoryUrl(category.slug)}>
                {category.name}
              </Link>
              <span className={category.productCount ? styles.meta : styles.metaEmpty}>
                {categoryCountText(category.productCount)}
              </span>
              {category.description ? (
                <p className={styles.description}>{category.description}</p>
              ) : null}
            </li>
          ))}
        </ul>
      </Container>
    </section>
  );
}

function CollectionSection({ data }: { data: HomeCollection }) {
  const { collection, others, products } = data;
  const href = collectionUrl(collection);
  return (
    <section aria-labelledby="home-collection" className={`${styles.section} ${styles.sectionAlt}`}>
      <Container>
        <div className={collection.image ? styles.collectionIntroWithImage : styles.collectionIntro}>
          <div className={styles.collectionCopy}>
            <p className={styles.eyebrow}>Подборка</p>
            <h2 id="home-collection">
              <Link href={href}>{collection.name}</Link>
            </h2>
            {collection.description ? (
              <p className={styles.collectionDescription}>{collection.description}</p>
            ) : null}
            <p className={styles.collectionCount}>{productCountText(collection.productCount)}</p>
            <Link className={styles.textLink} href={href}>
              Смотреть подборку <span aria-hidden="true">→</span>
            </Link>
          </div>
          {collection.image ? (
            <Link
              aria-label={`Открыть подборку: ${collection.name}`}
              className={styles.collectionImage}
              href={href}
            >
              <CatalogProductImage image={collection.image} name={`Подборка «${collection.name}»`} />
            </Link>
          ) : null}
        </div>

        {products.length ? (
          <>
            <p className={styles.basis}>В редакционном порядке</p>
            <ol aria-label={`Товары подборки «${collection.name}»`} className={styles.productGrid}>
              {products.map((product) => (
                <li key={product.id}>
                  <ProductCard headingLevel={3} product={product} />
                </li>
              ))}
            </ol>
          </>
        ) : null}

        <div className={styles.otherCollections}>
          {others.length ? (
            <>
              <h3 id="home-other-collections">Ещё подборки</h3>
              <ul aria-labelledby="home-other-collections">
                {others.map((item) => (
                  <li key={item.id}>
                    <Link href={collectionUrl(item)}>{item.name}</Link>
                  </li>
                ))}
              </ul>
            </>
          ) : null}
          <Link className={styles.textLink} href="/collections">
            Все подборки <span aria-hidden="true">→</span>
          </Link>
        </div>
      </Container>
    </section>
  );
}

function ManufacturersSection({ data }: { data: HomeManufacturers }) {
  return (
    <section aria-labelledby="home-manufacturers" className={styles.section}>
      <Container>
        <div className={styles.sectionHeader}>
          <div>
            <h2 id="home-manufacturers">Производители</h2>
            <p className={styles.basis}>По алфавиту</p>
          </div>
          <Link className={styles.textLink} href="/manufacturers">
            Все производители, A–Z / А–Я{" "}
            <span aria-hidden="true">→</span>
          </Link>
        </div>
        <ul className={styles.manufacturerList}>
          {data.manufacturers.map((manufacturer) => (
            <li className={styles.manufacturerItem} key={manufacturer.id}>
              <Link className={styles.itemName} href={manufacturerUrl(manufacturer.slug)}>
                {manufacturer.name}
              </Link>
              <span className={styles.meta}>
                {[manufacturer.country, productCountText(manufacturer.productCount)]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            </li>
          ))}
        </ul>
      </Container>
    </section>
  );
}

function ClosingNavigation() {
  return (
    <nav aria-label="Разделы магазина" className={styles.closingNav}>
      <Container>
        <ul>
          <li><Link href="/catalog">Каталог</Link></li>
          <li><Link href="/categories">Категории</Link></li>
          <li><Link href="/manufacturers">Производители</Link></li>
          <li><Link href="/collections">Подборки</Link></li>
        </ul>
      </Container>
    </nav>
  );
}

export function HomePage({ data }: { data: HomePageData }) {
  const categories =
    data.categories.status === "ready" && data.categories.data.categories.length
      ? data.categories.data
      : undefined;
  const collection = data.collection.status === "ready" ? data.collection.data : undefined;
  const manufacturers =
    data.manufacturers.status === "ready" && data.manufacturers.data.manufacturers.length
      ? data.manufacturers.data
      : undefined;
  const allUnavailable =
    data.categories.status === "unavailable" &&
    data.collection.status === "unavailable" &&
    data.manufacturers.status === "unavailable";

  return (
    <main className={styles.main} id="main-content">
      <HomeHero collection={collection?.collection} />
      {allUnavailable ? (
        <Container>
          <p className={styles.notice} role="status">
            Не удалось загрузить разделы каталога. Обновите страницу чуть позже.
          </p>
        </Container>
      ) : null}
      {categories ? <CategoriesSection data={categories} /> : null}
      {collection ? <CollectionSection data={collection} /> : null}
      {manufacturers ? <ManufacturersSection data={manufacturers} /> : null}
      <ClosingNavigation />
    </main>
  );
}
