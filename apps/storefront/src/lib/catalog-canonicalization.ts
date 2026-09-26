import type { CatalogData } from "./api/catalog";
import type { CatalogState } from "./catalog-state";

function matchesIdentity(
  item: { id: string; slug: string },
  identity: string | undefined,
): boolean {
  return Boolean(identity && (item.id === identity || item.slug === identity));
}

export function canonicalizeCatalogIdentity(
  state: CatalogState,
  data: Pick<
    CatalogData,
    "brands" | "categories" | "manufacturers" | "producerSeries"
  >,
): CatalogState {
  const corrected = { ...state };
  const category = data.categories.find((item) => matchesIdentity(item, state.category));
  if (state.category && !category) corrected.category = undefined;
  else if (category && state.category !== category.slug) corrected.category = category.slug;

  const manufacturer = data.manufacturers.find((item) =>
    matchesIdentity(item, state.manufacturer),
  );
  if (state.manufacturer && !manufacturer) {
    corrected.manufacturer = undefined;
    corrected.brand = undefined;
    corrected.series = undefined;
  } else if (manufacturer && state.manufacturer !== manufacturer.slug) {
    corrected.manufacturer = manufacturer.slug;
  }

  const brand = data.brands.find((item) => matchesIdentity(item, state.brand));
  if (
    state.brand &&
    (!brand || (manufacturer && brand.manufacturerId !== manufacturer.id))
  ) {
    corrected.brand = undefined;
    corrected.series = undefined;
  } else if (brand && state.brand !== brand.slug) {
    corrected.brand = brand.slug;
  }

  if (corrected.brand && state.series && data.producerSeries) {
    const series = data.producerSeries.find((item) =>
      [item.id, item.slug, item.name].some(
        (identity) => identity.toLocaleLowerCase("ru") === state.series?.toLocaleLowerCase("ru"),
      ),
    );
    corrected.series = series?.name;
  }

  return corrected;
}
