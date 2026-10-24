// Typed client for the B4 staff order API (api/admin-openapi.yaml). Components
// use these functions, never raw fetch. There is no delete and no arbitrary
// status assignment: only the lifecycle actions Go offers.

import { adminRequest, isRecord, type ApiResult, type MutationAuth } from "./admin-api";

export type OrderStatus = "processing" | "transit" | "delivered" | "cancelled";

export const orderStatuses: OrderStatus[] = ["processing", "transit", "delivered", "cancelled"];

// Filter labels; an order itself always shows the label Go stored with it.
export const orderStatusLabels: Record<OrderStatus, string> = {
  processing: "Собираем заказ",
  transit: "В пути",
  delivered: "Доставлен",
  cancelled: "Отменён",
};

export type OrderLine = {
  position: number;
  productId: string;
  sku: string;
  name: string;
  manufacturer: string;
  quantity: number;
  unitPriceMinor: number;
  lineTotalMinor: number;
};

export type AdminOrder = {
  id: string;
  number: string;
  revision: number;
  status: string;
  statusLabel: string;
  allowedTransitions: OrderStatus[];
  createdAt: string;
  deliveredAt?: string;
  customer: { id: string; name: string; email: string; phone: string };
  deliveryAddress: string;
  comment: string;
  itemCount: number;
  totalMinor: number;
  items?: OrderLine[];
};

export type OrderPage = { items: AdminOrder[]; total: number; page: number; limit: number; totalPages: number };

export type OrderQuery = { q: string; status: OrderStatus | ""; page: number; limit: number };

const ORDERS = "/api/admin/v1/orders";
const orderPath = (id: string) => `${ORDERS}/${encodeURIComponent(id)}`;

const isLine = (value: unknown): value is OrderLine =>
  isRecord(value) &&
  ["productId", "sku", "name", "manufacturer"].every((key) => typeof value[key] === "string") &&
  ["position", "quantity", "unitPriceMinor", "lineTotalMinor"].every((key) => Number.isSafeInteger(value[key]));

export function parseOrder(body: unknown): AdminOrder | null {
  if (!isRecord(body)) return null;
  const texts = ["id", "number", "status", "statusLabel", "createdAt", "deliveryAddress", "comment"];
  const numbers = ["revision", "itemCount", "totalMinor"];
  if (!texts.every((key) => typeof body[key] === "string") || !numbers.every((key) => Number.isSafeInteger(body[key]))) return null;
  if (!Array.isArray(body.allowedTransitions) || !body.allowedTransitions.every((status) => orderStatuses.includes(status as OrderStatus))) return null;
  const customer = body.customer;
  if (!isRecord(customer) || !["id", "name", "email", "phone"].every((key) => typeof customer[key] === "string")) return null;
  if (body.deliveredAt !== undefined && typeof body.deliveredAt !== "string") return null;
  if (body.items !== undefined && (!Array.isArray(body.items) || !body.items.every(isLine))) return null;
  return body as unknown as AdminOrder;
}

function parsePage(body: unknown): OrderPage | null {
  if (!isRecord(body) || !Array.isArray(body.items)) return null;
  const items = body.items.map(parseOrder);
  if (items.some((item) => item === null)) return null;
  if (!["total", "page", "limit", "totalPages"].every((key) => Number.isSafeInteger(body[key]))) return null;
  return { items: items as AdminOrder[], total: body.total as number, page: body.page as number, limit: body.limit as number, totalPages: body.totalPages as number };
}

export function listOrders(query: OrderQuery, signal?: AbortSignal): Promise<ApiResult<OrderPage>> {
  const params = new URLSearchParams({ page: String(query.page), limit: String(query.limit) });
  if (query.status) params.set("status", query.status);
  if (query.q) params.set("q", query.q);
  return adminRequest(`${ORDERS}?${params}`, parsePage, { signal });
}

export function getOrder(id: string, signal?: AbortSignal): Promise<ApiResult<AdminOrder>> {
  return adminRequest(orderPath(id), parseOrder, { signal });
}

/** transit or delivered; cancellation is cancelOrder. */
export function changeOrderStatus(id: string, expectedRevision: number, status: "transit" | "delivered", auth: MutationAuth): Promise<ApiResult<AdminOrder>> {
  return adminRequest(`${orderPath(id)}/status`, parseOrder, { method: "POST", body: { status, expectedRevision }, auth });
}

export function cancelOrder(id: string, expectedRevision: number, auth: MutationAuth): Promise<ApiResult<AdminOrder>> {
  return adminRequest(`${orderPath(id)}/cancel`, parseOrder, { method: "POST", body: { expectedRevision }, auth });
}
