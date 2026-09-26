import type { CatalogState } from "../catalog-state";
import { parseProduct, type Product } from "./product";
import {
  ApiRequestError,
  type ApiRequestOptions,
  isRecord,
  requestApiJson,
} from "./request";

export type Category = {
  id: string;
  slug: string;
  name: string;
  parentId?: string;
  productCount: number;
};

export type Manufacturer = {
  id: string;
  slug: string;
  name: string;
  country: string;
  description?: string;
  logo?: string;
  productCount: number;
};

export type Brand = {
  id: string;
  slug: string;
  name: string;
  manufacturerId: string;
  manufacturer: string;
  description?: string;
  logo?: string;
  productCount: number;
};

export type FacetValue = {
  id: string;
  slug: string;
  name: string;
  count: number;
};

export type CatalogFacets = {
  categories: FacetValue[];
  manufacturers: FacetValue[];
  brands: FacetValue[];
  tags: FacetValue[];
  series: FacetValue[];
  minPriceMinor: number;
  maxPriceMinor: number;
};

export type CatalogPage = {
  items: Product[];
  total: number;
  page: number;
  limit: number;
  totalPages: number;
};

export type CatalogData = {
  page: CatalogPage;
  facets: CatalogFacets;
  producerSeries?: FacetValue[];
  categories: Category[];
  manufacturers: Manufacturer[];
  brands: Brand[];
};

export class CatalogApiError extends Error {
  readonly kind: ApiRequestError["kind"];
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: ApiRequestError["kind"],
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "CatalogApiError";
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

function integer(record: Record<string, unknown>, key: string, minimum = 0): number {
  const value = record[key];
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    throw new Error(`${key} must be a safe integer greater than or equal to ${minimum}`);
  }
  return value;
}

function array(value: unknown, key: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`${key} must be an array`);
  return value;
}

function envelopeItems(value: unknown, key: string): unknown[] {
  if (!isRecord(value)) throw new Error(`${key} must be an object`);
  return array(value.items, `${key}.items`);
}

function parseCategory(value: unknown): Category {
  if (!isRecord(value)) throw new Error("category must be an object");
  return {
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    name: requiredString(value, "name"),
    parentId: optionalString(value, "parentId"),
    productCount: integer(value, "productCount"),
  };
}

export function parseManufacturer(value: unknown): Manufacturer {
  if (!isRecord(value)) throw new Error("manufacturer must be an object");
  return {
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    name: requiredString(value, "name"),
    country: requiredString(value, "country"),
    description: optionalString(value, "description"),
    logo: optionalString(value, "logo"),
    productCount: integer(value, "productCount"),
  };
}

export function parseBrand(value: unknown): Brand {
  if (!isRecord(value)) throw new Error("brand must be an object");
  return {
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    name: requiredString(value, "name"),
    manufacturerId: requiredString(value, "manufacturerId"),
    manufacturer: requiredString(value, "manufacturer"),
    description: optionalString(value, "description"),
    logo: optionalString(value, "logo"),
    productCount: integer(value, "productCount"),
  };
}

function parseFacet(value: unknown): FacetValue {
  if (!isRecord(value)) throw new Error("facet value must be an object");
  return {
    id: requiredString(value, "id"),
    slug: requiredString(value, "slug"),
    name: requiredString(value, "name"),
    count: integer(value, "count"),
  };
}

export function parseCatalogPage(value: unknown): CatalogPage {
  if (!isRecord(value)) throw new Error("catalog page must be an object");
  return {
    items: array(value.items, "items").map(parseProduct),
    total: integer(value, "total"),
    page: integer(value, "page", 1),
    limit: integer(value, "limit", 1),
    totalPages: integer(value, "totalPages"),
  };
}

export function parseCatalogFacets(value: unknown): CatalogFacets {
  if (!isRecord(value)) throw new Error("catalog facets must be an object");
  return {
    categories: array(value.categories, "categories").map(parseFacet),
    manufacturers: array(value.manufacturers, "manufacturers").map(parseFacet),
    brands: array(value.brands, "brands").map(parseFacet),
    tags: array(value.tags, "tags").map(parseFacet),
    series: array(value.series, "series").map(parseFacet),
    minPriceMinor: integer(value, "minPriceMinor"),
    maxPriceMinor: integer(value, "maxPriceMinor"),
  };
}

export function serializeCatalogRequest(state: CatalogState): string {
  const params = new URLSearchParams();
  if (state.query) params.set("q", state.query);
  if (state.category) params.set("category", state.category);
  if (state.manufacturer) params.set("manufacturer", state.manufacturer);
  if (state.brand) params.set("brand", state.brand);
  if (state.series) params.set("series", state.series);
  for (const tag of state.tags) params.append("tag", tag);
  if (state.minPriceMinor) params.set("minPriceMinor", String(state.minPriceMinor));
  if (state.maxPriceMinor) params.set("maxPriceMinor", String(state.maxPriceMinor));
  if (state.inStock) params.set("inStock", "true");
  if (state.onSale) params.set("sale", "true");
  if (state.newOnly) params.set("newOnly", "true");
  params.set("sort", state.sort);
  params.set("page", String(state.page));
  params.set("limit", String(state.pageSize));
  return params.toString();
}

export async function getCatalogData(
  state: CatalogState,
  options: ApiRequestOptions = {},
): Promise<CatalogData> {
  const query = serializeCatalogRequest(state);
  const producerParams = new URLSearchParams();
  if (state.manufacturer) producerParams.set("manufacturer", state.manufacturer);
  if (state.brand) producerParams.set("brand", state.brand);
  const producerSeriesRequest = state.brand && state.series
    ? requestApiJson(`/api/catalog/facets?${producerParams.toString()}`, options)
    : Promise.resolve(undefined);
  try {
    const [page, facets, categories, manufacturers, brands, producerFacets] = await Promise.all([
      requestApiJson(`/api/catalog?${query}`, options),
      requestApiJson(`/api/catalog/facets?${query}`, options),
      requestApiJson("/api/categories", options),
      requestApiJson("/api/manufacturers", options),
      requestApiJson("/api/brands", options),
      producerSeriesRequest,
    ]);
    return {
      page: parseCatalogPage(page),
      facets: parseCatalogFacets(facets),
      producerSeries: producerFacets
        ? parseCatalogFacets(producerFacets).series
        : undefined,
      categories: envelopeItems(categories, "categories").map(parseCategory),
      manufacturers: envelopeItems(manufacturers, "manufacturers").map(parseManufacturer),
      brands: envelopeItems(brands, "brands").map(parseBrand),
    };
  } catch (error) {
    if (error instanceof CatalogApiError) throw error;
    if (error instanceof ApiRequestError) {
      throw new CatalogApiError(error.kind, error.message, {
        cause: error,
        requestId: error.requestId,
        status: error.status,
      });
    }
    throw new CatalogApiError(
      "invalid-response",
      "The catalog response did not match its contract",
      { cause: error },
    );
  }
}
