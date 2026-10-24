import type { ProductQuery, ProductStatus } from "./products-api";

export const pageSizes = [25, 50, 100] as const;
const statuses: ProductStatus[] = ["active", "archived", "all"];

type SearchParams = Record<string, string | string[] | undefined>;

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value) ?? "";

/** The list state lives in the URL; unknown or malformed values fall back to defaults. */
export function parseProductQuery(params: SearchParams): ProductQuery {
  const status = first(params.status) as ProductStatus;
  const page = Number(first(params.page));
  const limit = Number(first(params.limit));
  return {
    q: first(params.q).trim().slice(0, 100),
    status: statuses.includes(status) ? status : "active",
    page: Number.isSafeInteger(page) && page >= 1 && page <= 10_000 ? page : 1,
    limit: (pageSizes as readonly number[]).includes(limit) ? limit : 50,
  };
}

/** The canonical URL of a list state, omitting defaults. */
export function productListHref(query: ProductQuery): string {
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.status !== "active") params.set("status", query.status);
  if (query.page !== 1) params.set("page", String(query.page));
  if (query.limit !== 50) params.set("limit", String(query.limit));
  const search = params.toString();
  return search ? `/products?${search}` : "/products";
}
