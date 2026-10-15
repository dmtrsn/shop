import Link from "next/link";

import { ButtonLink } from "@/components/ui/button";
import { Container } from "@/components/ui/container";
import { ArrowRightIcon } from "@/components/ui/icons";
import { Panel } from "@/components/ui/panel";
import { SectionHeader } from "@/components/ui/section-header";
import type { Category } from "@/lib/api/catalog";
import type { Collection } from "@/lib/api/collection";
import type {
  HomeCategories,
  HomeCollection,
  HomeManufacturers,
  HomePageData,
} from "@/lib/api/home";
import { categoryUrl } from "@/lib/category-tree";
import { productCountText } from "@/lib/plural";

import { categoryCountText } from "./category-navigation";
import { collectionUrl } from "./collection-navigation";
import { ProductCard } from "./product-card";
import { ScrollRail } from "./scroll-rail";
import styles from "./home-page.module.css";

export const HOME_TITLE = "Хороший вкус. Ничего лишнего.";

/** Frontend chrome owned by Next; catalogue media stays on the Go /assets host. */
export const HERO_IMAGE = { src: "/brand/hero-confectionery.webp", width: 1672, height: 941 };

function manufacturerUrl(slug: string): string {
  return `/manufacturers/${encodeURIComponent(slug)}`;
}

// Only the accents Go actually serves get a tint; anything else stays neutral.
const ACCENTS = new Set(["lime", "cocoa", "coral", "sky"]);
function accentClass(accent: string): string {
  return ACCENTS.has(accent) ? styles[`accent-${accent}`] : styles.accentNeutral;
}

export function HomeHero({ collection }: { collection?: HomeCollection["collection"] }) {
  return (
    <section aria-labelledby="home-title" className={styles.hero}>
      <Container>
        <div className={styles.heroCard}>
          {/* LCP element: a real image with intrinsic size, high priority, never lazy. */}
          <img
            alt=""
            className={styles.heroImage}
            decoding="async"
            fetchPriority="high"
            height={HERO_IMAGE.height}
            src={HERO_IMAGE.src}
            width={HERO_IMAGE.width}
          />
          <div aria-hidden="true" className={styles.heroScrim} />
          <div className={styles.heroCopy}>
            <p className={styles.heroEyebrow}>Кондитерские изделия</p>
            <h1 className={styles.heroTitle} id="home-title">{HOME_TITLE}</h1>
            <p className={styles.heroLede}>
              Шоколад, конфеты, печенье и пастила. Ищите по названию, артикулу, производителю или
              бренду — или начните с категории.
            </p>
            <div className={styles.heroActions}>
              <ButtonLink href="/catalog" variant="brand">
                Перейти в каталог <ArrowRightIcon />
              </ButtonLink>
              {collection ? (
                <ButtonLink className={styles.heroSecondary} href={collectionUrl(collection)} variant="secondary">
                  Подборка: {collection.name}
                </ButtonLink>
              ) : null}
            </div>
          </div>
        </div>
      </Container>
    </section>
  );
}

function CategoryTile({ category }: { category: Category }) {
  return (
    <li>
      <Panel as={Link} className={styles.categoryTile} href={categoryUrl(category.slug)} lift>
        {/* Neutral monogram from the real name: no per-category artwork is invented. */}
        <span aria-hidden="true" className={styles.monogram}>{category.name.charAt(0)}</span>
        <span className={styles.categoryText}>
          <strong>{category.name}</strong>
          <span className={category.productCount ? styles.meta : styles.metaEmpty}>
            {categoryCountText(category.productCount)}
          </span>
          {category.description ? <span className={styles.description}>{category.description}</span> : null}
        </span>
      </Panel>
    </li>
  );
}

function CategoriesSection({ data }: { data: HomeCategories }) {
  return (
    <section aria-labelledby="home-categories" className={styles.section}>
      <Container>
        <SectionHeader
          id="home-categories"
          kicker="Категории"
          link={{
            href: "/categories",
            label: data.totalRoots > data.categories.length ? `Все категории (${data.totalRoots})` : "Все категории",
          }}
          title="Найдите свой вкус"
        />
        <ul className={styles.categoryGrid}>
          {data.categories.map((category) => <CategoryTile category={category} key={category.id} />)}
        </ul>
      </Container>
    </section>
  );
}

function CollectionCard({ collection, featured }: { collection: Collection; featured: boolean }) {
  return (
    <li className={featured ? styles.collectionFeatured : undefined}>
      <Link className={`${styles.collectionCard} ${accentClass(collection.accent)}`} href={collectionUrl(collection)}>
        {collection.image ? (
          // Decorative: the card's own text names the collection.
          <img alt="" className={styles.collectionPhoto} loading="lazy" src={collection.image} />
        ) : null}
        <span className={styles.collectionEyebrow}>{featured ? "Главная подборка" : "Подборка"}</span>
        <h3 className={styles.collectionTitle}>{collection.name}</h3>
        {collection.description ? <span className={styles.collectionDescription}>{collection.description}</span> : null}
        <span className={styles.collectionCount}>
          {productCountText(collection.productCount)} <span aria-hidden="true">→</span>
        </span>
      </Link>
    </li>
  );
}

function CollectionSection({ data }: { data: HomeCollection }) {
  const { collection, others, products } = data;
  const cards = [collection, ...others];
  return (
    <section aria-labelledby="home-collections" className={styles.section}>
      <Container>
        <SectionHeader id="home-collections" kicker="Подборки" link={{ href: "/collections", label: "Все подборки" }} title="Выбрали за вас" />
        <ul className={styles.collectionGrid} data-count={cards.length}>
          {cards.map((item, index) => <CollectionCard collection={item} featured={index === 0} key={item.id} />)}
        </ul>
        {products.length ? (
          <>
            <p className={styles.basis}>Из подборки «{collection.name}», в редакционном порядке</p>
            <ol aria-label={`Товары подборки «${collection.name}»`} className={styles.productGrid}>
              {products.map((product) => (
                <li key={product.id}>
                  <ProductCard headingLevel={3} product={product} />
                </li>
              ))}
            </ol>
          </>
        ) : null}
      </Container>
    </section>
  );
}

function ManufacturersSection({ data }: { data: HomeManufacturers }) {
  return (
    <section aria-labelledby="home-manufacturers" className={styles.section}>
      <Container>
        <SectionHeader
          id="home-manufacturers"
          kicker="По алфавиту"
          link={{ href: "/manufacturers", label: "Все производители" }}
          title="Производители"
        />
        {/* A CSS scroll rail of links; a focused chip is brought fully into view. */}
        <ScrollRail className={styles.manufacturerRail}>
          {data.manufacturers.map((manufacturer) => (
            <li key={manufacturer.id}>
              <Panel as={Link} className={styles.manufacturerChip} href={manufacturerUrl(manufacturer.slug)} padding="none">
                <strong>{manufacturer.name}</strong>
                <span>
                  {[manufacturer.country, productCountText(manufacturer.productCount)].filter(Boolean).join(" · ")}
                </span>
              </Panel>
            </li>
          ))}
        </ScrollRail>
      </Container>
    </section>
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
            Не удалось загрузить разделы каталога. Обновите страницу чуть позже или{" "}
            <Link href="/catalog">откройте каталог</Link>.
          </p>
        </Container>
      ) : null}
      {categories ? <CategoriesSection data={categories} /> : null}
      {collection ? <CollectionSection data={collection} /> : null}
      {manufacturers ? <ManufacturersSection data={manufacturers} /> : null}
    </main>
  );
}
