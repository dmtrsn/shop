import { parseCart, type Cart } from "./cart";
import { isRecord } from "./json";
import { parseProduct, type Product } from "./product-dto";

export type RepeatLine = {
  sourcePosition: number; productId: string; sku: string; name: string; image: string;
  requestedQuantity: number; originalUnitPriceMinor: number; product?: Product;
  existingCartQuantity: number; existingCartSelected: boolean; suggestedQuantity: number; proposedQuantity: number;
  status: "available" | "limited" | "unavailable" | "discontinued";
  priceChanged: boolean; priceDeltaMinor?: number; replacement?: Product;
};
export type RepeatPreview = { orderId: string; orderNumber: string; items: RepeatLine[] };
export type RepeatSelection = {
  sourcePosition: number; productId: string; quantity: number; expectedExistingCartQuantity: number;
  expectedUnitPriceMinor: number; expectedCurrency: string; expectedMinimumQuantity: number; expectedQuantityStep: number;
};
export type RepeatBody = { items: readonly Readonly<RepeatSelection>[] };
export type RepeatReceipt = {
  sourceOrderId: string; sourceOrderNumber: string; addedCount: number; addedQuantity: number;
  additions: { sourcePosition: number; productId: string; quantity: number }[];
};
export type RepeatResult = { receipt: RepeatReceipt; cart: Cart; replayed: boolean };
const maxCount = 2147483647;
function record(value: unknown): Record<string, unknown> {
  if (!isRecord(value)) throw new Error("Invalid repeat object");
  return value;
}
function string(value: unknown, nonempty = false): string {
  if (typeof value !== "string" || (nonempty && !value.trim())) throw new Error("Invalid repeat string");
  return value;
}
function integer(value: unknown, min = 0, max = Number.MAX_SAFE_INTEGER): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < min || value > max) throw new Error("Invalid repeat integer");
  return value;
}
function boolean(value: unknown): boolean {
  if (typeof value !== "boolean") throw new Error("Invalid repeat boolean");
  return value;
}
function product(value: unknown): Product | undefined {
  if (value === undefined || value === null) return undefined;
  const parsed = parseProduct(value);
  if (!parsed.id || !parsed.slug) throw new Error("Invalid repeat product identity");
  integer(parsed.stock, 0, maxCount); integer(parsed.minimumQuantity, 1, maxCount); integer(parsed.quantityStep, 1, maxCount);
  return parsed;
}
export function parseRepeatPreview(body: unknown): RepeatPreview {
  const value = record(body);
  if (!Array.isArray(value.items)) throw new Error("Invalid repeat items");
  const positions = new Set<number>();
  const items = value.items.map((raw): RepeatLine => {
    const row = record(raw);
    const sourcePosition = integer(row.sourcePosition, -2147483648, maxCount);
    if (positions.has(sourcePosition)) throw new Error("Duplicate repeat source position");
    positions.add(sourcePosition);
    const status = row.status;
    if (status !== "available" && status !== "limited" && status !== "unavailable" && status !== "discontinued") throw new Error("Invalid repeat status");
    const current = product(row.product);
    const productId = string(row.productId, true);
    if ((status === "discontinued") !== !current || (current && current.id !== productId)) throw new Error("Invalid original product");
    return {
      sourcePosition, productId, sku: string(row.sku), name: string(row.name), image: string(row.image),
      requestedQuantity: integer(row.requestedQuantity, 1, maxCount), originalUnitPriceMinor: integer(row.originalUnitPriceMinor),
      product: current, existingCartQuantity: integer(row.existingCartQuantity, 0, maxCount),
      existingCartSelected: boolean(row.existingCartSelected), suggestedQuantity: integer(row.suggestedQuantity, 0, maxCount),
      proposedQuantity: integer(row.proposedQuantity, 0, maxCount), status, priceChanged: boolean(row.priceChanged),
      ...(row.priceDeltaMinor === undefined ? {} : { priceDeltaMinor: integer(row.priceDeltaMinor, -Number.MAX_SAFE_INTEGER) }),
      replacement: product(row.replacement),
    };
  });
  return { orderId: string(value.orderId, true), orderNumber: string(value.orderNumber, true), items };
}
export function parseRepeatReceipt(body: unknown): RepeatReceipt {
  const value = record(body);
  if (!Array.isArray(value.additions) || !value.additions.length) throw new Error("Invalid repeat additions");
  let previous = -Infinity;
  const additions = value.additions.map((raw) => {
    const row = record(raw);
    const sourcePosition = integer(row.sourcePosition, -2147483648, maxCount);
    if (sourcePosition <= previous) throw new Error("Invalid receipt positions");
    previous = sourcePosition;
    return { sourcePosition, productId: string(row.productId, true), quantity: integer(row.quantity, 1, maxCount) };
  });
  const addedCount = integer(value.addedCount, 1, maxCount);
  const addedQuantity = integer(value.addedQuantity, 1);
  if (new Set(additions.map((row) => row.productId)).size !== addedCount ||
      additions.reduce((sum, row) => sum + row.quantity, 0) !== addedQuantity) throw new Error("Invalid receipt counts");
  return { sourceOrderId: string(value.sourceOrderId, true), sourceOrderNumber: string(value.sourceOrderNumber, true), addedCount, addedQuantity, additions };
}
export function parseRepeatResult(body: unknown): Omit<RepeatResult, "replayed"> {
  const value = record(body);
  return { receipt: parseRepeatReceipt(value.receipt), cart: parseCart(value.cart) };
}
