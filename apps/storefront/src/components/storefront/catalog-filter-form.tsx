import type {
  Brand,
  CatalogFacets,
  Category,
  Manufacturer,
} from "@/lib/api/catalog";
import {
  catalogHiddenFields,
  formatCatalogPrice,
  type CatalogState,
} from "@/lib/catalog-state";

import styles from "./catalog.module.css";
import { ProducerDependencyGuard } from "./producer-dependency-guard";

type CatalogFilterFormProps = {
  brands: Brand[];
  categories: Category[];
  facets: CatalogFacets;
  manufacturers: Manufacturer[];
  prefix: string;
  state: CatalogState;
};

function matchesIdentity(
  item: { id: string; slug: string },
  identity: string | undefined,
): boolean {
  return Boolean(identity && (item.id === identity || item.slug === identity));
}

export function CatalogFilterForm({
  brands,
  categories,
  facets,
  manufacturers,
  prefix,
  state,
}: CatalogFilterFormProps) {
  const selectedManufacturer = manufacturers.find((item) =>
    matchesIdentity(item, state.manufacturer),
  );
  const selectedBrand = brands.find((item) => matchesIdentity(item, state.brand));
  const relevantBrands = selectedManufacturer
    ? brands.filter((brand) => brand.manufacturerId === selectedManufacturer.id)
    : selectedBrand
      ? [selectedBrand]
      : [];
  const canChooseBrand = Boolean(selectedManufacturer || state.brand);
  const canChooseSeries = Boolean(state.brand);
  const series = canChooseSeries
    ? Array.from(
        new Map(
          [
            ...facets.series,
            ...(state.series
              ? [{ id: state.series, slug: state.series, name: state.series, count: 0 }]
              : []),
          ].map((item) => [item.name, item]),
        ).values(),
      )
    : [];
  const facetTags = Array.from(
    new Map(
      [
        ...facets.tags,
        ...state.tags.map((tag) => ({ id: tag, slug: tag, name: tag, count: 0 })),
      ].map((item) => [item.name, item]),
    ).values(),
  );

  return (
    <form action="/catalog" className={styles.filterForm} method="get">
      {catalogHiddenFields(state, [
        "query",
        "category",
        "manufacturer",
        "brand",
        "series",
        "tags",
        "minPriceMinor",
        "maxPriceMinor",
        "inStock",
        "onSale",
        "newOnly",
      ]).map(([name, value], index) => (
        <input key={`${name}-${value}-${index}`} name={name} type="hidden" value={value} />
      ))}

      <div className={styles.filterGroup}>
        <label htmlFor={`${prefix}-q`}>Поиск</label>
        <input
          defaultValue={state.query}
          id={`${prefix}-q`}
          maxLength={200}
          name="q"
          placeholder="Название или артикул"
          type="search"
        />
      </div>

      <div className={styles.filterGroup}>
        <label htmlFor={`${prefix}-category`}>Категория</label>
        <select defaultValue={state.category ?? ""} id={`${prefix}-category`} name="category">
          <option value="">Все категории</option>
          {categories.map((category) => (
            <option key={category.id} value={category.slug}>
              {category.parentId ? "— " : ""}{category.name}
            </option>
          ))}
        </select>
      </div>

      <fieldset className={styles.filterFieldset}>
        <legend>Производитель, бренд и серия</legend>
        <label htmlFor={`${prefix}-manufacturer`}>Производитель</label>
        <select
          defaultValue={state.manufacturer ?? ""}
          id={`${prefix}-manufacturer`}
          name="manufacturer"
        >
          <option value="">Все производители</option>
          {manufacturers.map((manufacturer) => (
            <option key={manufacturer.id} value={manufacturer.slug}>
              {manufacturer.name}
            </option>
          ))}
        </select>

        <label htmlFor={`${prefix}-brand`}>Бренд</label>
        <select
          defaultValue={state.brand ?? ""}
          disabled={!canChooseBrand}
          id={`${prefix}-brand`}
          name="brand"
        >
          <option value="">Все бренды</option>
          {relevantBrands.map((brand) => (
            <option key={brand.id} value={brand.slug}>
              {brand.name}
            </option>
          ))}
        </select>
        {!canChooseBrand ? (
          <p className={styles.dependencyHint}>Сначала выберите и примените производителя.</p>
        ) : null}

        <label htmlFor={`${prefix}-series`}>Серия</label>
        <select
          defaultValue={state.series ?? ""}
          disabled={!canChooseSeries}
          id={`${prefix}-series`}
          name="series"
        >
          <option value="">Все серии</option>
          {series.map((item) => (
            <option key={item.id} value={item.name}>
              {item.name}{item.count ? ` · ${item.count}` : ""}
            </option>
          ))}
        </select>
        {!canChooseSeries ? (
          <p className={styles.dependencyHint}>Сначала выберите и примените бренд.</p>
        ) : null}
        <ProducerDependencyGuard
          brandSelectId={`${prefix}-brand`}
          manufacturerSelectId={`${prefix}-manufacturer`}
          seriesSelectId={`${prefix}-series`}
        />
      </fieldset>

      <fieldset className={styles.filterFieldset}>
        <legend>Цена, ₽</legend>
        <div className={styles.priceFields}>
          <label htmlFor={`${prefix}-min`}>
            <span>От</span>
            <input
              defaultValue={formatCatalogPrice(state.minPriceMinor)}
              id={`${prefix}-min`}
              inputMode="decimal"
              min="0"
              name="min"
              step="0.01"
              type="number"
            />
          </label>
          <label htmlFor={`${prefix}-max`}>
            <span>До</span>
            <input
              defaultValue={formatCatalogPrice(state.maxPriceMinor)}
              id={`${prefix}-max`}
              inputMode="decimal"
              min="0"
              name="max"
              step="0.01"
              type="number"
            />
          </label>
        </div>
      </fieldset>

      <fieldset className={styles.checkGroup}>
        <legend>Наличие и предложения</legend>
        <label>
          <input defaultChecked={state.inStock} name="stock" type="checkbox" value="1" />
          <span>Только в наличии</span>
        </label>
        <label>
          <input defaultChecked={state.onSale} name="sale" type="checkbox" value="1" />
          <span>Со сниженной ценой</span>
        </label>
        <label>
          <input defaultChecked={state.newOnly} name="new" type="checkbox" value="1" />
          <span>Новинки</span>
        </label>
      </fieldset>

      {facetTags.length ? (
        <fieldset className={styles.checkGroup}>
          <legend>Характеристики</legend>
          {facetTags.map((tag) => (
            <label key={tag.id}>
              <input
                defaultChecked={state.tags.includes(tag.name)}
                name="tag"
                type="checkbox"
                value={tag.name}
              />
              <span>{tag.name}</span>
              {tag.count ? <small>{tag.count}</small> : null}
            </label>
          ))}
        </fieldset>
      ) : null}

      <button className={styles.applyButton} type="submit">
        Показать товары
      </button>
    </form>
  );
}
