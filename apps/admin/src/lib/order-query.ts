import { orderStatuses, type OrderQuery, type OrderStatus } from "./orders-api";
import { pageSizes } from "./product-query";

type SearchParams = Record<string, string | string[] | undefined>;

const first = (value: string | string[] | undefined) => (Array.isArray(value) ? value[0] : value) ?? "";

/** The order list state lives in the URL; malformed values fall back to defaults. */
export function parseOrderQuery(params: SearchParams): OrderQuery {
  const status = first(params.status) as OrderStatus;
  const page = Number(first(params.page));
  const limit = Number(first(params.limit));
  return {
    q: first(params.q).trim().slice(0, 100),
    status: orderStatuses.includes(status) ? status : "",
    page: Number.isSafeInteger(page) && page >= 1 && page <= 10_000 ? page : 1,
    limit: (pageSizes as readonly number[]).includes(limit) ? limit : 50,
  };
}

export function orderListHref(query: OrderQuery): string {
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.status) params.set("status", query.status);
  if (query.page !== 1) params.set("page", String(query.page));
  if (query.limit !== 50) params.set("limit", String(query.limit));
  const search = params.toString();
  return search ? `/orders?${search}` : "/orders";
}
