import { getProduct, ProductApiError, type Product } from "./product";
import {
  ApiRequestError,
  type ApiRequestOptions,
  isRecord,
  requestApiJsonWithMeta,
} from "./request";

export type Collection = {
  id: string;
  slug: string;
  name: string;
  description: string;
  accent: string;
  image?: string;
  productIds: string[];
  productCount: number;
  startsAt?: string;
  endsAt?: string;
};

export type CollectionPageData = {
  collection: Collection;
  products: Product[];
};

export type CollectionApiErrorKind =
  | "cancelled"
  | "configuration"
  | "invalid-response"
  | "unavailable";

export class CollectionApiError extends Error {
  readonly kind: CollectionApiErrorKind;
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: CollectionApiErrorKind,
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "CollectionApiError";
    this.kind = kind;
    this.requestId = options.requestId;
    this.status = options.status;
  }
}

function requiredString(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value !== "string") throw new Error(`${key} must be a string`);
  return value;
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${key} must be a string`);
  return value || undefined;
}

function nonNegativeInteger(record: Record<string, unknown>, key: string): number {
  const value = record[key];
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${key} must be a non-negative safe integer`);
  }
  return value;
}

function stringArray(record: Record<string, unknown>, key: string): string[] {
  const value = record[key];
  if (value === null) return [];
  if (!Array.isArray(value) || !value.every((item) => typeof item === "string")) {
    throw new Error(`${key} must be an array of strings`);
  }
  return value;
}

function timestamp(record: Record<string, unknown>, key: string): string | undefined {
  const value = requiredString(record, key);
  if (value.startsWith("0001-01-01T")) return undefined;
  if (Number.isNaN(Date.parse(value))) throw new Error(`${key} must be a timestamp`);
  return value;
}

export function parseCollection(value: unknown): Collection {
  if (!isRecord(value)) throw new Error("collection must be an object");
  return {
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    name: requiredString(value, "name"),
    description: requiredString(value, "description"),
    accent: requiredString(value, "accent"),
    image: optionalString(value, "image"),
    productIds: stringArray(value, "productIds"),
    productCount: nonNegativeInteger(value, "productCount"),
    startsAt: timestamp(value, "startsAt"),
    endsAt: timestamp(value, "endsAt"),
  };
}

function collectionError(error: unknown): CollectionApiError {
  if (error instanceof CollectionApiError) return error;
  if (error instanceof ApiRequestError) {
    return new CollectionApiError(error.kind, error.message, {
      cause: error,
      requestId: error.requestId,
      status: error.status,
    });
  }
  if (error instanceof ProductApiError) {
    return new CollectionApiError(
      error.kind === "not-found" ? "invalid-response" : error.kind,
      error.kind === "not-found"
        ? "The collection references a product that does not exist"
        : error.message,
      { cause: error, requestId: error.requestId, status: error.status },
    );
  }
  return new CollectionApiError(
    "invalid-response",
    "The collection response did not match its contract",
    { cause: error },
  );
}

export async function getCollections(
  options: ApiRequestOptions = {},
): Promise<Collection[]> {
  let response: Awaited<ReturnType<typeof requestApiJsonWithMeta>>;
  try {
    response = await requestApiJsonWithMeta("/api/collections", options);
  } catch (error) {
    throw collectionError(error);
  }

  try {
    if (!isRecord(response.body) || !Array.isArray(response.body.items)) {
      throw new Error("collections must contain an items array");
    }
    return response.body.items.map(parseCollection);
  } catch (error) {
    throw new CollectionApiError(
      "invalid-response",
      "The collection response did not match its contract",
      { cause: error, requestId: response.requestId, status: response.status },
    );
  }
}

function matchesIdentity(collection: Collection, identity: string): boolean {
  return collection.id === identity || collection.slug === identity;
}

export const COLLECTION_PREVIEW_LIMIT = 4;

// Catalog filtering by collection does not preserve editorial order and there
// is no ids filter, so a fixed, small number of product lookups keeps the
// productIds order exact.
export async function getCollectionProducts(
  collection: Collection,
  options: ApiRequestOptions = {},
  limit = COLLECTION_PREVIEW_LIMIT,
): Promise<Product[]> {
  try {
    return await Promise.all(
      collection.productIds.slice(0, limit).map((productId) => getProduct(productId, options)),
    );
  } catch (error) {
    throw collectionError(error);
  }
}

export async function getCollectionPageData(
  identity: string,
  options: ApiRequestOptions = {},
): Promise<CollectionPageData | undefined> {
  try {
    const collections = await getCollections(options);
    const collection = collections.find((item) => matchesIdentity(item, identity));
    if (!collection) return undefined;

    const products = await getCollectionProducts(collection, options);
    return { collection, products };
  } catch (error) {
    throw collectionError(error);
  }
}
