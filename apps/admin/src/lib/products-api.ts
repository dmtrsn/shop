// Typed client for the B1 staff product API and the catalog options it needs
// (api/admin-openapi.yaml). Components use these functions, never raw fetch.

import { adminRequest, isRecord, type ApiResult, type MutationAuth } from "./admin-api";

export type Reference = { id: string; name: string };

export type AdminProduct = {
  id: string;
  revision: number;
  archived: boolean;
  archivedAt?: string;
  slug: string;
  sku: string;
  name: string;
  shortName: string;
  description: string;
  priceMinor: number;
  oldPriceMinor: number;
  currency: string;
  unit: string;
  weightGrams: number;
  image: string;
  gallery: string[];
  category: Reference;
  manufacturer: Reference;
  brand: Reference;
  collections: Reference[];
  series: string;
  tags: string[];
  badges: string[];
  ingredients: string;
  nutrition: string;
  shelfLifeDays: number;
  rating: number;
  reviewsCount: number;
  stock: number;
  minimumQuantity: number;
  quantityStep: number;
  featured: boolean;
  createdAt: string;
  updatedAt: string;
};

export type ProductPage = { items: AdminProduct[]; total: number; page: number; limit: number; totalPages: number };

export type ProductStatus = "active" | "archived" | "all";

export type ProductQuery = { q: string; status: ProductStatus; page: number; limit: number };

/** The B1 write document: every member is required. */
export type ProductPayload = {
  slug: string;
  sku: string;
  name: string;
  shortName: string;
  description: string;
  priceMinor: number;
  oldPriceMinor: number;
  unit: string;
  weightGrams: number;
  categoryId: string;
  manufacturerId: string;
  brandId: string;
  collectionIds: string[];
  series: string;
  tags: string[];
  badges: string[];
  ingredients: string;
  nutrition: string;
  shelfLifeDays: number;
  stock: number;
  minimumQuantity: number;
  quantityStep: number;
  featured: boolean;
};

export type CatalogOptions = {
  categories: Array<Reference & { parentId?: string }>;
  manufacturers: Reference[];
  brands: Array<Reference & { manufacturerId: string }>;
  collections: Reference[];
};

const PRODUCTS = "/api/admin/v1/products";

function isReference(value: unknown): value is Reference {
  return isRecord(value) && typeof value.id === "string" && typeof value.name === "string";
}

const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every((item) => typeof item === "string");

// Checks the members the UI relies on; the contract test pins the rest.
export function parseProduct(body: unknown): AdminProduct | null {
  if (!isRecord(body)) return null;
  const textFields = ["id", "slug", "sku", "name", "shortName", "description", "currency", "unit", "image", "series", "ingredients", "nutrition", "createdAt", "updatedAt"];
  const numberFields = ["revision", "priceMinor", "oldPriceMinor", "weightGrams", "shelfLifeDays", "rating", "reviewsCount", "stock", "minimumQuantity", "quantityStep"];
  if (!textFields.every((key) => typeof body[key] === "string") || !numberFields.every((key) => Number.isSafeInteger(body[key]) || (key === "rating" && typeof body[key] === "number"))) {
    return null;
  }
  if (typeof body.archived !== "boolean" || typeof body.featured !== "boolean" || !strings(body.gallery) || !strings(body.tags) || !strings(body.badges)) return null;
  if (!isReference(body.category) || !isReference(body.manufacturer) || !isReference(body.brand) || !Array.isArray(body.collections) || !body.collections.every(isReference)) {
    return null;
  }
  if (body.archivedAt !== undefined && typeof body.archivedAt !== "string") return null;
  return body as unknown as AdminProduct;
}

function parsePage(body: unknown): ProductPage | null {
  if (!isRecord(body) || !Array.isArray(body.items)) return null;
  const items = body.items.map(parseProduct);
  if (items.some((item) => item === null)) return null;
  const numbers = ["total", "page", "limit", "totalPages"].every((key) => Number.isSafeInteger(body[key]));
  return numbers ? { items: items as AdminProduct[], total: body.total as number, page: body.page as number, limit: body.limit as number, totalPages: body.totalPages as number } : null;
}

function parseOptions(body: unknown): CatalogOptions | null {
  if (!isRecord(body)) return null;
  const { categories, manufacturers, brands, collections } = body;
  if (!Array.isArray(categories) || !categories.every((c) => isReference(c) && ["undefined", "string"].includes(typeof (c as Record<string, unknown>).parentId))) return null;
  if (!Array.isArray(manufacturers) || !manufacturers.every(isReference)) return null;
  if (!Array.isArray(brands) || !brands.every((b) => isReference(b) && typeof (b as Record<string, unknown>).manufacturerId === "string")) return null;
  if (!Array.isArray(collections) || !collections.every(isReference)) return null;
  return body as unknown as CatalogOptions;
}

const productPath = (id: string) => `${PRODUCTS}/${encodeURIComponent(id)}`;

export function listProducts(query: ProductQuery, signal?: AbortSignal): Promise<ApiResult<ProductPage>> {
  const params = new URLSearchParams({ status: query.status, page: String(query.page), limit: String(query.limit) });
  if (query.q) params.set("q", query.q);
  return adminRequest(`${PRODUCTS}?${params}`, parsePage, { signal });
}

export function getProduct(id: string, signal?: AbortSignal): Promise<ApiResult<AdminProduct>> {
  return adminRequest(productPath(id), parseProduct, { signal });
}

export function getCatalogOptions(signal?: AbortSignal): Promise<ApiResult<CatalogOptions>> {
  return adminRequest("/api/admin/v1/catalog-options", parseOptions, { signal });
}

export function createProduct(payload: ProductPayload, auth: MutationAuth): Promise<ApiResult<AdminProduct>> {
  return adminRequest(PRODUCTS, parseProduct, { method: "POST", body: payload, auth });
}

export function updateProduct(id: string, expectedRevision: number, payload: ProductPayload, auth: MutationAuth): Promise<ApiResult<AdminProduct>> {
  return adminRequest(productPath(id), parseProduct, { method: "PUT", body: { ...payload, expectedRevision }, auth });
}

export function archiveProduct(id: string, expectedRevision: number, auth: MutationAuth): Promise<ApiResult<AdminProduct>> {
  return adminRequest(`${productPath(id)}/archive`, parseProduct, { method: "POST", body: { expectedRevision }, auth });
}

export function restoreProduct(id: string, expectedRevision: number, auth: MutationAuth): Promise<ApiResult<AdminProduct>> {
  return adminRequest(`${productPath(id)}/restore`, parseProduct, { method: "POST", body: { expectedRevision }, auth });
}
