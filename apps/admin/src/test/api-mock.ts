import { vi } from "vitest";

import type { AdminOrder } from "@/lib/orders-api";
import type { AdminProduct, CatalogOptions } from "@/lib/products-api";

export const testStaff = { id: "stf_" + "a".repeat(32), email: "ops@example.com", displayName: "Оператор" };

export type Call = { method: string; url: string; headers: Headers; body: unknown };
type Responder = (call: Call) => Response | Promise<Response>;

/**
 * A fetch stand-in routed by "METHOD /path" (the path without the query).
 * Each route answers with its queued responders in order, repeating the last.
 * The staff session read answers with an authenticated session unless
 * overridden. Every call is recorded.
 */
export function mockApi(routes: Record<string, Responder | Responder[]> = {}) {
  const calls: Call[] = [];
  const queues = new Map(Object.entries(routes).map(([key, value]) => [key, Array.isArray(value) ? [...value] : [value]]));
  const fetcher = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = String(input);
    const method = init.method ?? "GET";
    const call: Call = { method, url, headers: new Headers(init.headers), body: typeof init.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    const key = `${method} ${url.split("?")[0]}`;
    const queue = queues.get(key);
    if (!queue) {
      if (key === "GET /api/admin/v1/auth/session") return Response.json({ authenticated: true, staff: { ...testStaff, createdAt: "2026-10-01T00:00:00Z" }, expiresAt: "x", csrfToken: "csrf-1" });
      throw new Error(`unexpected request ${key}`);
    }
    const responder = queue.length > 1 ? queue.shift()! : queue[0];
    return responder(call);
  });
  vi.stubGlobal("fetch", fetcher);
  return { calls, fetcher, called: (key: string) => calls.filter((call) => `${call.method} ${call.url.split("?")[0]}` === key) };
}

export const json = (body: unknown, status = 200) => () => Response.json(body, { status });
export const apiError = (status: number, code: string, fields?: Array<{ field: string; message: string }>) => () =>
  Response.json({ error: { code, message: "server text", requestId: "req-1", ...(fields ? { fields } : {}) } }, { status });

export function product(overrides: Partial<AdminProduct> = {}): AdminProduct {
  return {
    id: "p01", revision: 7, archived: false, slug: "dark-bar", sku: "CHA-701", name: "Тёмный шоколад", shortName: "Тёмный",
    description: "Описание", priceMinor: 48900, oldPriceMinor: 54900, currency: "RUB", unit: "плитка 90 г", weightGrams: 90,
    image: "/assets/product-chocolate.webp", gallery: [], category: { id: "cat-bars", name: "Плитки" },
    manufacturer: { id: "m-choco", name: "Choco Atelier" }, brand: { id: "b-noir", name: "Atelier Noir" },
    collections: [{ id: "col-best", name: "Хиты" }], series: "", tags: ["веган"], badges: ["Хит"], ingredients: "", nutrition: "",
    shelfLifeDays: 180, rating: 4.9, reviewsCount: 142, stock: 24, minimumQuantity: 1, quantityStep: 1, featured: true,
    createdAt: "2026-01-10T00:00:00Z", updatedAt: "2026-10-20T10:00:00Z",
    ...overrides,
  };
}

export const catalogOptions: CatalogOptions = {
  categories: [{ id: "cat-choc", name: "Шоколад" }, { id: "cat-bars", name: "Плитки", parentId: "cat-choc" }],
  manufacturers: [{ id: "m-choco", name: "Choco Atelier" }, { id: "m-caffarel", name: "Caffarel" }],
  brands: [
    { id: "b-noir", name: "Atelier Noir", manufacturerId: "m-choco" },
    { id: "b-caffarel", name: "Caffarel", manufacturerId: "m-caffarel" },
  ],
  collections: [{ id: "col-best", name: "Хиты" }, { id: "col-gifts", name: "Подарки" }],
};

export function order(overrides: Partial<AdminOrder> = {}): AdminOrder {
  return {
    id: "ord-2026-10483", number: "#10483", revision: 1, status: "processing", statusLabel: "Собираем заказ",
    allowedTransitions: ["transit", "cancelled"], createdAt: "2026-10-24T09:30:00Z",
    customer: { id: "usr-demo", name: "Алексей Демо", email: "demo@sugar.ru", phone: "+7 900 000-00-00" },
    deliveryAddress: "Москва, ул. Большая Дмитровка, 12, кв. 4", comment: "Позвонить за час", itemCount: 5, totalMinor: 247000,
    items: [
      { position: 1, productId: "p03", sku: "SWE-204", name: "Розовый шоколад с малиной", manufacturer: "Sweetzerland", quantity: 2, unitPriceMinor: 71900, lineTotalMinor: 143800 },
      { position: 2, productId: "p05", sku: "LEO-505", name: "Ассорти пралине", manufacturer: "Leonidas", quantity: 3, unitPriceMinor: 24400, lineTotalMinor: 73200 },
    ],
    ...overrides,
  };
}
