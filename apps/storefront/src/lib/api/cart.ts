import { isRecord } from "./json";
import { parseProduct, type Product } from "./product-dto";

export type CartItem = {
  product: Product;
  quantity: number;
  selected: boolean;
  available: number;
  lineTotalMinor: number;
  issue?: string;
};

export type CartGroup = {
  manufacturer: Product["manufacturer"];
  items: CartItem[];
  selectedCount: number;
  subtotalMinor: number;
};

export type CartSummary = {
  itemCount: number;
  positionCount: number;
  selectedCount: number;
  subtotalMinor: number;
  discountMinor: number;
  deliveryMinor: number;
  totalMinor: number;
  unavailableCount: number;
};

// updatedAt is display metadata only; it is not a revision.
export type Cart = { items: CartItem[]; groups: CartGroup[]; summary: CartSummary; updatedAt: string };

const summaryFields = [
  "itemCount", "positionCount", "selectedCount", "subtotalMinor",
  "discountMinor", "deliveryMinor", "totalMinor", "unavailableCount",
] as const;

// Money and counts are Go-authoritative. Unsafe JS integers are rejected rather
// than displayed with silently corrupted precision.
function integer(value: unknown, minimum = 0): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    throw new Error("Invalid cart integer");
  }
  return value;
}

function parseItem(value: unknown): CartItem {
  if (!isRecord(value) || typeof value.selected !== "boolean" ||
      (value.issue !== undefined && typeof value.issue !== "string")) {
    throw new Error("Invalid cart item");
  }
  const product = parseProduct(value.product);
  if (!product.id || !product.slug) throw new Error("Invalid cart product identity");
  return {
    product,
    quantity: integer(value.quantity, 1),
    selected: value.selected,
    available: integer(value.available),
    lineTotalMinor: integer(value.lineTotalMinor),
    ...(typeof value.issue === "string" ? { issue: value.issue } : {}),
  };
}

function parseTimestamp(value: unknown): string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T/.test(value) || !Number.isFinite(Date.parse(value))) {
    throw new Error("Invalid cart timestamp");
  }
  return value;
}

export function parseCart(body: unknown): Cart {
  if (!isRecord(body) || !Array.isArray(body.items) || !Array.isArray(body.groups) || !isRecord(body.summary)) {
    throw new Error("Invalid cart response");
  }
  const updatedAt = parseTimestamp(body.updatedAt);
  const items = body.items.map(parseItem);
  const byID = new Map(items.map((line) => [line.product.id, line]));
  if (byID.size !== items.length) throw new Error("Duplicate cart product");

  // Groups repeat full lines. Each must match its canonical item exactly and
  // every item must appear in exactly one group of its own manufacturer.
  const grouped = new Set<string>();
  const manufacturers = new Set<string>();
  const groups = body.groups.map((raw): CartGroup => {
    if (!isRecord(raw) || !isRecord(raw.manufacturer) || !Array.isArray(raw.items)) {
      throw new Error("Invalid cart group");
    }
    const { id, slug, name, country } = raw.manufacturer;
    if (typeof id !== "string" || !id || typeof slug !== "string" || typeof name !== "string" ||
        typeof country !== "string" || manufacturers.has(id)) {
      throw new Error("Invalid cart manufacturer");
    }
    manufacturers.add(id);
    const lines = raw.items.map(parseItem).map((line) => {
      const canonical = byID.get(line.product.id);
      if (!canonical || grouped.has(line.product.id) || line.product.manufacturer.id !== id ||
          JSON.stringify(canonical) !== JSON.stringify(line)) {
        throw new Error("Inconsistent cart groups");
      }
      grouped.add(line.product.id);
      return canonical;
    });
    return {
      manufacturer: { id, slug, name, country },
      items: lines,
      selectedCount: integer(raw.selectedCount),
      subtotalMinor: integer(raw.subtotalMinor),
    };
  });
  if (grouped.size !== items.length) throw new Error("Incomplete cart groups");

  const rawSummary = body.summary;
  const summary = Object.fromEntries(summaryFields.map((key) => [key, integer(rawSummary[key])])) as CartSummary;
  if (summary.positionCount !== items.length) throw new Error("Invalid cart position count");
  return { items, groups, summary, updatedAt };
}
