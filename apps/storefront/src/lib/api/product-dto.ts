import { isRecord } from "./json";

export type ProductEntity = {
  id: string;
  slug: string;
  name: string;
};

export type Product = {
  id: string;
  slug: string;
  sku: string;
  name: string;
  shortName?: string;
  description: string;
  priceMinor: number;
  oldPriceMinor?: number;
  currency: string;
  unit: string;
  weightGrams?: number;
  image: string;
  gallery: string[];
  /** Original URL -> smaller derived copy (ADR 022); read only through thumbnailFor. */
  thumbnails?: Record<string, string>;
  category: ProductEntity;
  manufacturer: ProductEntity & { country: string };
  brand: ProductEntity & { manufacturerId: string; manufacturer: string };
  series?: string;
  tags: string[];
  badges: string[];
  ingredients?: string;
  nutrition?: string;
  shelfLifeDays?: number;
  stock: number;
  minimumQuantity: number;
  quantityStep: number;
};

function requiredString(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value !== "string") {
    throw new Error(`${key} must be a string`);
  }
  return value;
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${key} must be a string`);
  return value || undefined;
}

function requiredInteger(
  record: Record<string, unknown>,
  key: string,
  minimum = 0,
): number {
  const value = record[key];
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    throw new Error(`${key} must be a safe integer greater than or equal to ${minimum}`);
  }
  return value as number;
}

function optionalInteger(
  record: Record<string, unknown>,
  key: string,
  minimum = 0,
): number | undefined {
  if (record[key] === undefined) return undefined;
  return requiredInteger(record, key, minimum);
}

function stringArray(record: Record<string, unknown>, key: string): string[] {
  const value = record[key];
  if (value === undefined) return [];
  if (!Array.isArray(value) || !value.every((item) => typeof item === "string")) {
    throw new Error(`${key} must be an array of strings`);
  }
  return value;
}

function entity(value: unknown, key: string): ProductEntity & Record<string, unknown> {
  if (!isRecord(value)) throw new Error(`${key} must be an object`);
  return {
    ...value,
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    name: requiredString(value, "name"),
  };
}

// managedMedia is only a presentation hint here: image and gallery are already
// resolved by Go. A missing or malformed entry costs the thumbnail, never the
// product, so mixed-version backends keep rendering originals.
function parseThumbnails(value: unknown): Record<string, string> | undefined {
  if (!isRecord(value)) return undefined;
  const thumbnails: Record<string, string> = {};
  for (const item of [value.primary, ...(Array.isArray(value.gallery) ? value.gallery : [])]) {
    if (!isRecord(item)) continue;
    const { url, thumbnailUrl } = item;
    if (typeof url === "string" && url && typeof thumbnailUrl === "string" && thumbnailUrl) {
      thumbnails[url] = thumbnailUrl;
    }
  }
  return Object.keys(thumbnails).length ? thumbnails : undefined;
}

/**
 * The thumbnail of one of the product's current image URLs, if Go reported one.
 * Small surfaces try it before the original; large images and order snapshots
 * never call this.
 */
export function thumbnailFor(product: Pick<Product, "thumbnails">, url: string): string | undefined {
  const thumbnails = product.thumbnails;
  return thumbnails && Object.hasOwn(thumbnails, url) ? thumbnails[url] : undefined;
}

export function parseProduct(value: unknown): Product {
  if (!isRecord(value)) throw new Error("product must be an object");

  const category = entity(value.category, "category");
  const manufacturer = entity(value.manufacturer, "manufacturer");
  const brand = entity(value.brand, "brand");
  const currency = requiredString(value, "currency");
  if (!/^[A-Z]{3}$/.test(currency)) throw new Error("currency must be an ISO-style code");

  return {
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    sku: requiredString(value, "sku"),
    name: requiredString(value, "name"),
    shortName: optionalString(value, "shortName"),
    description: requiredString(value, "description"),
    priceMinor: requiredInteger(value, "priceMinor"),
    oldPriceMinor: optionalInteger(value, "oldPriceMinor"),
    currency,
    unit: requiredString(value, "unit"),
    weightGrams: optionalInteger(value, "weightGrams"),
    // Go resolves managed media once for all current-product consumers.
    // Order-item images use their separate immutable snapshot DTO.
    image: requiredString(value, "image"),
    gallery: stringArray(value, "gallery"),
    thumbnails: parseThumbnails(value.managedMedia),
    category,
    manufacturer: {
      id: manufacturer.id,
      slug: manufacturer.slug,
      name: manufacturer.name,
      country: requiredString(manufacturer, "country"),
    },
    brand: {
      id: brand.id,
      slug: brand.slug,
      name: brand.name,
      manufacturerId: requiredString(brand, "manufacturerId"),
      manufacturer: requiredString(brand, "manufacturer"),
    },
    series: optionalString(value, "series"),
    tags: stringArray(value, "tags"),
    badges: stringArray(value, "badges"),
    ingredients: optionalString(value, "ingredients"),
    nutrition: optionalString(value, "nutrition"),
    shelfLifeDays: optionalInteger(value, "shelfLifeDays"),
    stock: requiredInteger(value, "stock"),
    minimumQuantity: requiredInteger(value, "minimumQuantity", 1),
    quantityStep: requiredInteger(value, "quantityStep", 1),
  };
}
