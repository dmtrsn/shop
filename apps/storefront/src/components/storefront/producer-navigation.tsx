import Link from "next/link";

import { Container } from "@/components/ui/container";
import type { ManufacturerPageData } from "@/lib/api/producer";
import { producerCatalogUrl } from "@/lib/producer-navigation";

import { ManufacturerDirectory } from "./manufacturer-directory";
import styles from "./producer-navigation.module.css";

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
  return `${count} ${noun} в каталоге`;
}

export function ManufacturersPage({
  manufacturers,
}: {
  manufacturers: ManufacturerPageData["manufacturer"][];
}) {
  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page">Производители</span>
        </nav>
        <header className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Производители</p>
          <h1>От фабрик до мастерских</h1>
          <p>
            Найдите производителя по названию или алфавиту, затем перейдите к его брендам и товарам.
          </p>
        </header>
        {manufacturers.length ? (
          <ManufacturerDirectory manufacturers={manufacturers} />
        ) : (
          <div className={styles.pageEmpty}>
            <h2>Производителей пока нет</h2>
            <p>В каталоге ещё не опубликован справочник производителей.</p>
            <Link href="/catalog">Открыть каталог</Link>
          </div>
        )}
      </Container>
    </main>
  );
}

export function ManufacturerPage({ data }: { data: ManufacturerPageData }) {
  const { manufacturer, brands } = data;
  const allProductsUrl = producerCatalogUrl({ manufacturer: manufacturer.slug });

  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <Link href="/manufacturers">Производители</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page">{manufacturer.name}</span>
        </nav>

        <header className={styles.identity}>
          <div>
            <p className={styles.eyebrow}>Производитель</p>
            <h1>{manufacturer.name}</h1>
            {manufacturer.description ? <p className={styles.description}>{manufacturer.description}</p> : null}
          </div>
          <dl>
            {manufacturer.country ? <div><dt>Страна</dt><dd>{manufacturer.country}</dd></div> : null}
            <div><dt>Ассортимент</dt><dd>{productCountText(manufacturer.productCount)}</dd></div>
          </dl>
          <Link className={styles.primaryLink} href={allProductsUrl}>
            Все товары производителя
          </Link>
        </header>

        <section aria-labelledby="manufacturer-brands" className={styles.brandSection}>
          <div className={styles.sectionHeading}>
            <div>
              <p className={styles.eyebrow}>Навигация по ассортименту</p>
              <h2 id="manufacturer-brands">Бренды</h2>
            </div>
            <Link href={allProductsUrl}>Смотреть все товары →</Link>
          </div>

          {brands.length ? (
            <div className={styles.brandList}>
              {brands.map((brand) => {
                const brandUrl = producerCatalogUrl({
                  manufacturer: manufacturer.slug,
                  brand: brand.slug,
                });
                return (
                  <article className={styles.brand} key={brand.id}>
                    <div className={styles.brandIdentity}>
                      <div>
                        <h3>{brand.name}</h3>
                        {brand.description ? <p>{brand.description}</p> : null}
                        <span>{productCountText(brand.productCount)}</span>
                      </div>
                      <Link href={brandUrl}>Товары бренда →</Link>
                    </div>
                  </article>
                );
              })}
            </div>
          ) : (
            <div className={styles.pageEmpty}>
              <h3>Бренды пока не представлены</h3>
              <p>У производителя нет видимых брендов, но его товары можно проверить в каталоге.</p>
              <Link href={allProductsUrl}>Открыть товары производителя</Link>
            </div>
          )}
        </section>
      </Container>
    </main>
  );
}
