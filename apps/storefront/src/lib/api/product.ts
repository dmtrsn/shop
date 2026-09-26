import {
  ApiRequestError,
  type ApiRequestOptions,
  isRecord,
  requestApiJsonWithMeta,
} from "./request";

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

export type ProductApiErrorKind =
  | "cancelled"
  | "configuration"
  | "invalid-response"
  | "not-found"
  | "unavailable";

export class ProductApiError extends Error {
  readonly kind: ProductApiErrorKind;
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: ProductApiErrorKind,
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "ProductApiError";
    this.kind = kind;
    this.requestId = options.requestId;
    this.status = options.status;
  }
}

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
    image: requiredString(value, "image"),
    gallery: stringArray(value, "gallery"),
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

export async function getProduct(
  identity: string,
  options: ApiRequestOptions = {},
): Promise<Product> {
  let response: Awaited<ReturnType<typeof requestApiJsonWithMeta>>;
  try {
    response = await requestApiJsonWithMeta(
      `/api/products/${encodeURIComponent(identity)}`,
      options,
    );
  } catch (error) {
    if (error instanceof ApiRequestError) {
      if (error.status === 404) {
        throw new ProductApiError("not-found", "Product not found", {
          cause: error,
          requestId: error.requestId,
          status: error.status,
        });
      }
      throw new ProductApiError(error.kind, error.message, {
        cause: error,
        requestId: error.requestId,
        status: error.status,
      });
    }
    throw error;
  }

  try {
    return parseProduct(response.body);
  } catch (error) {
    throw new ProductApiError("invalid-response", "The product response did not match its contract", {
      cause: error,
      requestId: response.requestId,
      status: response.status,
    });
  }
}
