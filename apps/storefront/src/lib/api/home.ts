import { buildCategoryTree } from "../category-tree";
import { groupManufacturers } from "../producer-navigation";
import type { Category, Manufacturer } from "./catalog";
import { getCategories } from "./category";
import { type Collection, getCollectionProducts, getCollections } from "./collection";
import { getManufacturers } from "./producer";
import type { Product } from "./product";
import type { ApiRequestOptions } from "./request";

export const HOME_CATEGORY_LIMIT = 8;
export const HOME_MANUFACTURER_LIMIT = 8;
export const HOME_OTHER_COLLECTION_LIMIT = 3;

/** An optional homepage section; a failed or empty one is simply not rendered. */
export type HomeSection<T> = { status: "ready"; data: T } | { status: "unavailable" };

export type HomeCategories = {
  categories: Category[];
  totalRoots: number;
};

export type HomeCollection = {
  collection: Collection;
  products: Product[];
  others: Collection[];
};

export type HomeManufacturers = {
  manufacturers: Manufacturer[];
};

export type HomePageData = {
  categories: HomeSection<HomeCategories>;
  collection: HomeSection<HomeCollection | undefined>;
  manufacturers: HomeSection<HomeManufacturers>;
};

/** Root categories in API order; children stay on the category pages. */
export function selectHomeCategories(categories: Category[]): HomeCategories {
  const roots = buildCategoryTree(categories).roots.map((node) => node.category);
  return { categories: roots.slice(0, HOME_CATEGORY_LIMIT), totalRoots: roots.length };
}

/**
 * The first collection in the current API response that has products. Go has
 * already applied the schedule, and no ordering guarantee beyond the response
 * itself is assumed; nothing is inferred from names or counts.
 */
export function selectFeaturedCollection(collections: Collection[]): Collection | undefined {
  return collections.find((collection) => collection.productIds.length > 0);
}

/** Manufacturers with products, alphabetically in the /manufacturers directory order. */
export function selectHomeManufacturers(manufacturers: Manufacturer[]): HomeManufacturers {
  const withProducts = groupManufacturers(
    manufacturers.filter((manufacturer) => manufacturer.productCount > 0),
  ).flatMap((group) => group.manufacturers);
  return { manufacturers: withProducts.slice(0, HOME_MANUFACTURER_LIMIT) };
}

async function loadCollection(options: ApiRequestOptions): Promise<HomeCollection | undefined> {
  const collections = await getCollections(options);
  const collection = selectFeaturedCollection(collections);
  if (!collection) return undefined;
  const products = await getCollectionProducts(collection, options);
  const others = collections
    .filter((item) => item.id !== collection.id)
    .slice(0, HOME_OTHER_COLLECTION_LIMIT);
  return { collection, products, others };
}

function section<T>(result: PromiseSettledResult<T>): HomeSection<T> {
  return result.status === "fulfilled"
    ? { status: "ready", data: result.value }
    : { status: "unavailable" };
}

/**
 * At most 7 backend requests: categories, collections and manufacturers in
 * parallel, then up to four product lookups for the featured collection.
 * Sections fail independently and never fall back to fixture data.
 */
export async function getHomePageData(options: ApiRequestOptions = {}): Promise<HomePageData> {
  const [categories, collection, manufacturers] = await Promise.allSettled([
    getCategories(options).then(selectHomeCategories),
    loadCollection(options),
    getManufacturers(options).then(selectHomeManufacturers),
  ]);
  return {
    categories: section(categories),
    collection: section(collection),
    manufacturers: section(manufacturers),
  };
}
