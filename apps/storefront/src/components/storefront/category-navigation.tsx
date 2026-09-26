import Link from "next/link";

import { Container } from "@/components/ui/container";
import type { Category } from "@/lib/api/catalog";
import type { CategoryPageData } from "@/lib/api/category";
import { pluralRu, productCountText } from "@/lib/plural";
import {
  buildCategoryTree,
  categoryCatalogUrl,
  categoryUrl,
} from "@/lib/category-tree";

import styles from "./category-navigation.module.css";

export const DIRECTORY_CHILD_LIMIT = 8;

// productCount follows the current catalog contract (the category plus its
// direct children), so it is labelled neutrally rather than as a subtree total.
export function categoryCountText(count: number): string {
  if (count === 0) return "Пока нет товаров";
  return `${productCountText(count)} в каталоге`;
}

function subcategoryCountText(count: number): string {
  return `${count} ${pluralRu(count, ["подкатегория", "подкатегории", "подкатегорий"])}`;
}

function Count({ count }: { count: number }) {
  return (
    <span className={count ? styles.count : styles.countEmpty}>
      {categoryCountText(count)}
    </span>
  );
}

export function CategoriesPage({ categories }: { categories: Category[] }) {
  const { roots } = buildCategoryTree(categories);

  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page">Категории</span>
        </nav>

        <header className={styles.directoryHeading}>
          <p className={styles.eyebrow}>Каталог</p>
          <h1>Категории</h1>
          <p>
            Выберите вид сладостей, затем уточните подкатегорию или сразу откройте товары в каталоге.
          </p>
          <Link className={styles.textLink} href="/catalog">
            Все товары <span aria-hidden="true">→</span>
          </Link>
        </header>

        {roots.length ? (
          <ol className={styles.directory}>
            {roots.map(({ category, children }) => {
              const visibleChildren = children.slice(0, DIRECTORY_CHILD_LIMIT);
              const headingId = `category-${category.id}`;
              return (
                <li className={styles.directoryItem} key={category.id}>
                  <section aria-labelledby={headingId}>
                    <div className={styles.rootIdentity}>
                      <h2 id={headingId}>
                        <Link href={categoryUrl(category.slug)}>{category.name}</Link>
                      </h2>
                      <Count count={category.productCount} />
                    </div>
                    {category.description ? (
                      <p className={styles.rootDescription}>{category.description}</p>
                    ) : null}
                    {visibleChildren.length ? (
                      <ul aria-label={`Подкатегории: ${category.name}`} className={styles.childList}>
                        {visibleChildren.map(({ category: child }) => (
                          <li key={child.id}>
                            <Link href={categoryUrl(child.slug)}>{child.name}</Link>
                            <Count count={child.productCount} />
                          </li>
                        ))}
                      </ul>
                    ) : null}
                    {children.length > visibleChildren.length ? (
                      <Link className={styles.textLink} href={categoryUrl(category.slug)}>
                        Все подкатегории ({children.length}) <span aria-hidden="true">→</span>
                      </Link>
                    ) : null}
                  </section>
                </li>
              );
            })}
          </ol>
        ) : (
          <div className={styles.state}>
            <h2>Категорий пока нет</h2>
            <p>Справочник категорий ещё не опубликован. Все доступные товары остаются в каталоге.</p>
            <Link href="/catalog">Открыть каталог</Link>
          </div>
        )}
      </Container>
    </main>
  );
}

function CategoryRows({
  categories,
  currentId,
  label,
}: {
  categories: Category[];
  currentId?: string;
  label: string;
}) {
  return (
    <ul aria-label={label} className={styles.rows}>
      {categories.map((category) => {
        const current = category.id === currentId;
        return (
          <li className={styles.row} key={category.id}>
            <div className={styles.rowIdentity}>
              {current ? (
                <span aria-current="page" className={styles.rowCurrent}>{category.name}</span>
              ) : (
                <Link className={styles.rowName} href={categoryUrl(category.slug)}>{category.name}</Link>
              )}
              <Count count={category.productCount} />
            </div>
            <Link
              aria-label={`Товары: ${category.name}`}
              className={styles.rowAction}
              href={categoryCatalogUrl(category.slug)}
            >
              Товары <span aria-hidden="true">→</span>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

export function CategoryPage({ data }: { data: CategoryPageData }) {
  const { category, ancestors, parent, children, siblings } = data;
  const allProductsUrl = categoryCatalogUrl(category.slug);

  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.detailContainer}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link>
          <span aria-hidden="true">/</span>
          <Link href="/categories">Категории</Link>
          {ancestors.map((ancestor) => (
            <span className={styles.crumb} key={ancestor.id}>
              <span aria-hidden="true">/</span>
              <Link href={categoryUrl(ancestor.slug)}>{ancestor.name}</Link>
            </span>
          ))}
          <span aria-hidden="true">/</span>
          <span aria-current="page">{category.name}</span>
        </nav>

        <header className={styles.identity}>
          <div>
            <p className={styles.eyebrow}>Категория</p>
            <h1>{category.name}</h1>
            {category.description ? <p className={styles.description}>{category.description}</p> : null}
          </div>
          <dl>
            <div><dt>Ассортимент</dt><dd>{categoryCountText(category.productCount)}</dd></div>
            {children.length ? (
              <div><dt>Подкатегории</dt><dd>{subcategoryCountText(children.length)}</dd></div>
            ) : null}
          </dl>
          <Link className={styles.primaryLink} href={allProductsUrl}>
            Все товары категории
          </Link>
        </header>

        {children.length ? (
          <section aria-labelledby="category-children" className={styles.section}>
            <div className={styles.sectionHeading}>
              <h2 id="category-children">Подкатегории</h2>
              <Link href={allProductsUrl}>Смотреть все товары →</Link>
            </div>
            <CategoryRows categories={children} label={`Подкатегории: ${category.name}`} />
          </section>
        ) : (
          <section aria-labelledby="category-siblings" className={styles.section}>
            <div className={styles.sectionHeading}>
              <h2 id="category-siblings">
                {parent ? `Рядом в «${parent.name}»` : "Другие категории"}
              </h2>
              {parent ? (
                <Link href={categoryUrl(parent.slug)}>← {parent.name}</Link>
              ) : (
                <Link href="/categories">Все категории</Link>
              )}
            </div>
            <CategoryRows
              categories={siblings}
              currentId={category.id}
              label={parent ? `Категории в «${parent.name}»` : "Категории верхнего уровня"}
            />
          </section>
        )}
      </Container>
    </main>
  );
}
