import Link from "next/link";

import { Container } from "@/components/ui/container";
import type { CatalogData } from "@/lib/api/catalog";
import {
  catalogHiddenFields,
  catalogUrl,
  type CatalogState,
} from "@/lib/catalog-state";
import {
  ancestorsOf,
  buildCategoryTree,
  categoryUrl,
  findCategoryNode,
  rootOf,
} from "@/lib/category-tree";

import { CatalogFilterForm } from "./catalog-filter-form";
import { CompactProductCard, ProductCard } from "./product-card";
import styles from "./catalog.module.css";

type CatalogViewProps = {
  data: CatalogData;
  state: CatalogState;
};

const sortLabels: Record<CatalogState["sort"], string> = {
  name: "По названию",
  new: "Сначала новые",
  price_asc: "Сначала дешевле",
  price_desc: "Сначала дороже",
};

function formatMoney(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat("ru-RU", {
    currency,
    style: "currency",
  }).format(amountMinor / 100);
}

function pluralProducts(total: number): string {
  const mod100 = total % 100;
  const mod10 = total % 10;
  if (mod100 >= 11 && mod100 <= 14) return "товаров";
  if (mod10 === 1) return "товар";
  if (mod10 >= 2 && mod10 <= 4) return "товара";
  return "товаров";
}

function ActiveFilters({ data, state }: CatalogViewProps) {
  const category = data.categories.find(
    (item) => item.id === state.category || item.slug === state.category,
  );
  const manufacturer = data.manufacturers.find(
    (item) => item.id === state.manufacturer || item.slug === state.manufacturer,
  );
  const brand = data.brands.find(
    (item) => item.id === state.brand || item.slug === state.brand,
  );
  const filters: Array<{ key: string; label: string; href: string }> = [];
  if (state.query) filters.push({ key: "query", label: `Поиск: ${state.query}`, href: catalogUrl(state, { query: undefined }) });
  if (state.category) filters.push({ key: "category", label: category?.name ?? state.category, href: catalogUrl(state, { category: undefined }) });
  if (state.manufacturer) filters.push({ key: "manufacturer", label: manufacturer?.name ?? state.manufacturer, href: catalogUrl(state, { manufacturer: undefined, brand: undefined, series: undefined }) });
  if (state.brand) filters.push({ key: "brand", label: brand?.name ?? state.brand, href: catalogUrl(state, { brand: undefined, series: undefined }) });
  if (state.collection) filters.push({ key: "collection", label: `Подборка: ${state.collection}`, href: catalogUrl(state, { collection: undefined }) });
  if (state.series) filters.push({ key: "series", label: `Серия: ${state.series}`, href: catalogUrl(state, { series: undefined }) });
  state.tags.forEach((tag) => filters.push({ key: `tag-${tag}`, label: tag, href: catalogUrl(state, { tags: state.tags.filter((item) => item !== tag) }) }));
  if (state.minPriceMinor) filters.push({ key: "min", label: `От ${formatMoney(state.minPriceMinor, "RUB")}`, href: catalogUrl(state, { minPriceMinor: undefined }) });
  if (state.maxPriceMinor) filters.push({ key: "max", label: `До ${formatMoney(state.maxPriceMinor, "RUB")}`, href: catalogUrl(state, { maxPriceMinor: undefined }) });
  if (state.inStock) filters.push({ key: "stock", label: "В наличии", href: catalogUrl(state, { inStock: false }) });
  if (state.onSale) filters.push({ key: "sale", label: "Сниженная цена", href: catalogUrl(state, { onSale: false }) });
  if (state.newOnly) filters.push({ key: "new", label: "Новинки", href: catalogUrl(state, { newOnly: false }) });

  if (!filters.length) return null;
  const cleared = catalogUrl(state, {
    query: undefined,
    category: undefined,
    manufacturer: undefined,
    brand: undefined,
    collection: undefined,
    series: undefined,
    tags: [],
    minPriceMinor: undefined,
    maxPriceMinor: undefined,
    inStock: false,
    onSale: false,
    newOnly: false,
  });
  return (
    <div aria-label="Активные фильтры" className={styles.activeFilters}>
      {filters.map((filter) => (
        <Link href={filter.href} key={filter.key}>
          {filter.label}<span aria-hidden="true"> ×</span>
          <span className="visually-hidden"> — убрать фильтр</span>
        </Link>
      ))}
      <Link className={styles.clearFilters} href={cleared}>Очистить всё</Link>
    </div>
  );
}

function Pagination({ state, total, totalPages }: { state: CatalogState; total: number; totalPages: number }) {
  if (totalPages <= 1) return null;
  const start = (state.page - 1) * state.pageSize + 1;
  const end = Math.min(state.page * state.pageSize, total);
  const pages = Array.from(
    new Set([1, state.page - 1, state.page, state.page + 1, totalPages]),
  ).filter((page) => page >= 1 && page <= totalPages).sort((a, b) => a - b);

  return (
    <nav aria-label="Страницы каталога" className={styles.pagination}>
      <p>{start}–{end} из {total}</p>
      <div>
        {state.page > 1 ? (
          <Link href={catalogUrl(state, { page: state.page - 1 }, { resetPage: false })}>Назад</Link>
        ) : <span aria-disabled="true">Назад</span>}
        {pages.map((page, index) => {
          const previous = pages[index - 1];
          return (
            <span className={styles.pageItem} key={page}>
              {previous && page - previous > 1 ? <span aria-hidden="true">…</span> : null}
              <Link aria-current={page === state.page ? "page" : undefined} href={catalogUrl(state, { page }, { resetPage: false })}>{page}</Link>
            </span>
          );
        })}
        {state.page < totalPages ? (
          <Link href={catalogUrl(state, { page: state.page + 1 }, { resetPage: false })}>Далее</Link>
        ) : <span aria-disabled="true">Далее</span>}
      </div>
    </nav>
  );
}

export function CatalogView({ data, state }: CatalogViewProps) {
  const categoryTree = buildCategoryTree(data.categories);
  const categoryNode = findCategoryNode(categoryTree, state.category);
  const category = categoryNode?.category;
  const activeFilterCount = [
    state.query,
    state.category,
    state.manufacturer,
    state.brand,
    state.collection,
    state.series,
    ...state.tags,
    state.minPriceMinor,
    state.maxPriceMinor,
    state.inStock,
    state.onSale,
    state.newOnly,
  ].filter(Boolean).length;
  const topCategories = categoryTree.roots.map((node) => node.category);
  const activeTopCategoryId = categoryNode ? rootOf(categoryNode).category.id : undefined;

  return (
    <main className={styles.main} id="main-content">
      <Container className={styles.container}>
        <nav aria-label="Хлебные крошки" className={styles.breadcrumbs}>
          <Link href="/">Главная</Link><span aria-hidden="true">/</span>
          {categoryNode ? (
            <>
              <Link href="/catalog">Каталог</Link>
              {ancestorsOf(categoryNode).map(({ category: ancestor }) => (
                <span className={styles.crumb} key={ancestor.id}>
                  <span aria-hidden="true">/</span>
                  <Link href={categoryUrl(ancestor.slug)}>{ancestor.name}</Link>
                </span>
              ))}
              <span aria-hidden="true">/</span>
              <span aria-current="page">{categoryNode.category.name}</span>
            </>
          ) : <span aria-current="page">Каталог</span>}
        </nav>

        <header className={styles.heading}>
          <div>
            <p className={styles.eyebrow}>Кондитерские изделия</p>
            <h1>{category?.name ?? "Каталог"}</h1>
            <p>Категории и производители остаются независимыми — сочетайте их для точного выбора.</p>
          </div>
          <p className={styles.resultCount} aria-live="polite">
            {data.page.total} {pluralProducts(data.page.total)}
          </p>
        </header>

        <div className={styles.categoryHeading}>
          <span aria-hidden="true">Категории</span>
          <Link href="/categories">Все категории<span aria-hidden="true"> →</span></Link>
        </div>
        {topCategories.length ? (
          <nav aria-label="Основные категории" className={styles.categoryNavigation}>
            <Link aria-current={!state.category ? "page" : undefined} href={catalogUrl(state, { category: undefined })}>Все товары</Link>
            {topCategories.map((item) => (
              <Link
                aria-current={item.id === activeTopCategoryId ? "page" : undefined}
                href={catalogUrl(state, { category: item.slug })}
                key={item.id}
              >
                {item.name}
              </Link>
            ))}
          </nav>
        ) : null}

        <ActiveFilters data={data} state={state} />

        <div className={styles.mobileControls}>
          <details className={styles.mobileFilters}>
            <summary>Фильтры{activeFilterCount ? ` · ${activeFilterCount}` : ""}</summary>
            <CatalogFilterForm {...data} prefix="mobile-filter" state={state} />
          </details>
        </div>

        <div className={styles.catalogLayout}>
          <aside aria-label="Фильтры каталога" className={styles.filterRail}>
            <CatalogFilterForm {...data} prefix="desktop-filter" state={state} />
          </aside>

          <section aria-labelledby="catalog-results-heading" className={styles.results}>
            <div className={styles.toolbar}>
              <h2 className="visually-hidden" id="catalog-results-heading">Товары</h2>
              <form action="/catalog" className={styles.sortForm} method="get">
                {catalogHiddenFields(state, ["sort"]).map(([name, value], index) => (
                  <input key={`${name}-${value}-${index}`} name={name} type="hidden" value={value} />
                ))}
                <label htmlFor="catalog-sort">Сортировка</label>
                <select defaultValue={state.sort} id="catalog-sort" name="sort">
                  {Object.entries(sortLabels).map(([value, label]) => (
                    <option key={value} value={value}>{label}</option>
                  ))}
                </select>
                <button type="submit">Применить</button>
              </form>
              <div aria-label="Плотность каталога" className={styles.density} role="group">
                <Link aria-pressed={state.view === "comfortable"} href={catalogUrl(state, { view: "comfortable" }, { resetPage: false })} role="button">Обзор</Link>
                <Link aria-pressed={state.view === "compact"} href={catalogUrl(state, { view: "compact" }, { resetPage: false })} role="button">Список</Link>
              </div>
              <form action="/catalog" className={styles.sizeForm} method="get">
                {catalogHiddenFields(state, ["pageSize"]).map(([name, value], index) => (
                  <input key={`${name}-${value}-${index}`} name={name} type="hidden" value={value} />
                ))}
                <label htmlFor="catalog-size">На странице</label>
                <select defaultValue={state.pageSize} id="catalog-size" name="size">
                  <option value="24">24</option><option value="48">48</option><option value="96">96</option>
                </select>
                <button type="submit">Применить</button>
              </form>
            </div>

            {data.page.items.length ? (
              <>
                {state.view === "compact" ? (
                  <div aria-hidden="true" className={styles.compactHeader}>
                    <span>Товар</span><span>Производитель / бренд / серия</span><span>Упаковка</span><span>Наличие</span><span>Цена</span>
                  </div>
                ) : null}
                <ol className={state.view === "compact" ? styles.compactList : styles.productGrid}>
                {data.page.items.map((product) => (
                  <li key={product.id}>
                    {state.view === "compact" ? <CompactProductCard product={product} /> : <ProductCard product={product} />}
                  </li>
                ))}
                </ol>
              </>
            ) : (
              <div className={styles.emptyState}>
                <p className={styles.eyebrow}>0 товаров</p>
                <h2>Ничего не найдено</h2>
                <p>Уберите один или несколько фильтров или измените поисковый запрос.</p>
                <Link href={catalogUrl(state, {
                  query: undefined, category: undefined, manufacturer: undefined,
                  brand: undefined, collection: undefined, series: undefined, tags: [], minPriceMinor: undefined,
                  maxPriceMinor: undefined, inStock: false, onSale: false, newOnly: false,
                })}>Очистить фильтры</Link>
              </div>
            )}

            <Pagination state={state} total={data.page.total} totalPages={data.page.totalPages} />
          </section>
        </div>
      </Container>
    </main>
  );
}
