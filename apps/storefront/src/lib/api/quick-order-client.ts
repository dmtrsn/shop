import { compareProducts } from "../quick-order";
import { browserRequest, type BrowserRequestOptions } from "./browser-request";
import { parseCatalogPage } from "./catalog";
import type { Product } from "./product-dto";

const PAGE_SIZE = 100;
const CONCURRENCY = 4;

export async function loadManufacturerProducts(manufacturerId: string, options: BrowserRequestOptions = {}): Promise<Product[]> {
  if (!manufacturerId.trim()) throw new Error("Manufacturer ID is required");
  const controller = new AbortController();
  const signal = options.signal ? AbortSignal.any([options.signal, controller.signal]) : controller.signal;
  async function page(number: number) {
    signal.throwIfAborted();
    const params = new URLSearchParams({ manufacturer: manufacturerId, sort: "name", limit: String(PAGE_SIZE), page: String(number) });
    const data = parseCatalogPage(await browserRequest(`/api/catalog?${params}`, { ...options, signal }));
    signal.throwIfAborted();
    if (data.page !== number || data.limit !== PAGE_SIZE || data.totalPages !== Math.ceil(data.total / data.limit)) {
      throw new Error("Invalid catalog pagination metadata");
    }
    return data;
  }
  try {
    const first = await page(1);
    const pages: Product[][] = [first.items];
    let next = 2;
    let last = first.totalPages;
    async function worker() {
      while (next <= last) {
        const current = next++;
        const result = await page(current);
        pages[current - 1] = result.items;
        last = Math.max(last, result.totalPages);
      }
    }
    await Promise.all(Array.from({ length: Math.min(CONCURRENCY, Math.max(0, last - 1)) }, () => worker()));
    signal.throwIfAborted();
    // Offset pages are not a snapshot: concurrent catalog edits can still omit rows.
    const unique = new Map<string, Product>();
    for (const items of pages) for (const product of items) if (!unique.has(product.id)) unique.set(product.id, product);
    return [...unique.values()].sort(compareProducts);
  } catch (error) {
    controller.abort();
    if (options.signal?.aborted) options.signal.throwIfAborted();
    throw error;
  }
}
