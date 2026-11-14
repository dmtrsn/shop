import type { Manufacturer } from "./api/catalog";
import type { Product } from "./api/product-dto";

const collator = new Intl.Collator("ru", { sensitivity: "base", numeric: true });
const compareId = (a: string, b: string) => a < b ? -1 : a > b ? 1 : 0;

export function normalizeQuickOrderText(value: string): string {
  return value.normalize("NFKC").toLocaleLowerCase("ru").replace(/ё/g, "е")
    .replace(/[\p{P}\p{S}]+/gu, " ").replace(/\s+/gu, " ").trim();
}

function matchesTokens(text: string, query: string): boolean {
  const tokens = normalizeQuickOrderText(query).split(" ").filter(Boolean);
  const normalized = normalizeQuickOrderText(text);
  return tokens.every((token) => normalized.includes(token));
}

export function matchesManufacturer(manufacturer: Pick<Manufacturer, "name">, query: string): boolean {
  return matchesTokens(manufacturer.name, query);
}

export function manufacturerSortKey(name: string): string {
  const full = normalizeQuickOrderText(name);
  // Require a token boundary after the legal form; strip at most one prefix.
  const withoutPrefix = name.trim().replace(/^(?:ип|ооо|ао)(?=$|[\s\p{P}\p{S}])/iu, "");
  return normalizeQuickOrderText(withoutPrefix) || full;
}

export function compareManufacturers(a: Pick<Manufacturer, "id" | "name">, b: Pick<Manufacturer, "id" | "name">): number {
  return collator.compare(manufacturerSortKey(a.name), manufacturerSortKey(b.name))
    || collator.compare(normalizeQuickOrderText(a.name), normalizeQuickOrderText(b.name))
    || compareId(a.id, b.id);
}

export function compareProducts(a: Pick<Product, "id" | "name">, b: Pick<Product, "id" | "name">): number {
  return collator.compare(a.name, b.name) || compareId(a.id, b.id);
}

export function matchesProduct(product: Pick<Product, "name" | "sku">, query: string): boolean {
  const normalized = normalizeQuickOrderText(query);
  if (!normalized) return true;
  // SKU searches are a single compact identifier, not a mixture of name and SKU tokens.
  return matchesTokens(product.name, query)
    || (!/\s/u.test(query.trim()) && normalizeQuickOrderText(product.sku).replace(/ /g, "").includes(normalized.replace(/ /g, "")));
}

export type QuantityRules = Pick<Product, "minimumQuantity" | "quantityStep" | "stock">;
export type QuantityState = "empty" | "invalid-integer" | "below-minimum" | "invalid-step" | "above-stock" | "valid";
export type QuantityResult = { state: QuantityState; purchasable: boolean; rules: QuantityRules; quantity?: number; suggestion?: number };

export function checkQuantity(draft: string, rules: QuantityRules): QuantityResult {
  const { minimumQuantity: min, quantityStep: step, stock } = rules;
  const purchasable = stock >= min;
  const result: QuantityResult = { state: "empty", purchasable, rules };
  const input = draft.trim();
  if (!input) return result;
  const quantity = Number(input);
  if (!/^-?\d+$/.test(input) || !Number.isSafeInteger(quantity)) return { ...result, state: "invalid-integer" };
  let state: QuantityState = "valid";
  if (quantity < min) state = "below-minimum";
  else if ((quantity - min) % step !== 0) state = "invalid-step";
  else if (quantity > stock) state = "above-stock";
  let suggestion: number | undefined;
  if (state !== "valid" && quantity > 0 && purchasable) {
    const highest = min + Math.floor((stock - min) / step) * step;
    const upper = quantity <= min ? min : min + Math.ceil((quantity - min) / step) * step;
    suggestion = Number.isSafeInteger(upper) && upper <= stock ? upper : highest;
  }
  return { ...result, state, quantity, ...(suggestion === undefined ? {} : { suggestion }) };
}

export function adjustQuantity(draft: string, direction: 1 | -1, rules: QuantityRules): number | undefined {
  const result = checkQuantity(draft, rules);
  if (result.state === "empty") return direction === 1 && result.purchasable ? rules.minimumQuantity : undefined;
  if (result.state !== "valid" || result.quantity === undefined) return undefined;
  const next = result.quantity + direction * rules.quantityStep;
  return Number.isSafeInteger(next) && next >= rules.minimumQuantity && next <= rules.stock ? next : undefined;
}
