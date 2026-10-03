import { isRecord } from "./json";

// Historical snapshot of a purchased line, exactly as Go returns it. There is
// no slug: a product link uses the canonical ID and may lead to a 404 when the
// product was removed later.
export type OrderItem = {
  productId: string;
  sku: string;
  name: string;
  image: string;
  manufacturerId: string;
  manufacturer: string;
  quantity: number;
  unitPriceMinor: number;
  lineTotalMinor: number;
};

// Go stores only the total (including delivery for new checkouts), never a
// subtotal, discount, delivery or currency breakdown.
export type Order = {
  id: string;
  number: string;
  status: string;
  statusLabel: string;
  items: OrderItem[];
  itemCount: number;
  totalMinor: number;
  deliveryAddress: string;
  comment?: string;
  createdAt: string;
  // Go always serializes deliveredAt; the zero time means "not delivered".
  deliveredAt: string | null;
};

function text(value: unknown, nonempty = false): string {
  if (typeof value !== "string" || (nonempty && !value)) throw new Error("Invalid order text");
  return value;
}

// Money and counts are Go-authoritative; unsafe integers are rejected rather
// than displayed with silently corrupted precision.
function integer(value: unknown, minimum: number): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    throw new Error("Invalid order integer");
  }
  return value;
}

function timestamp(value: unknown): string {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T/.test(value) || !Number.isFinite(Date.parse(value))) {
    throw new Error("Invalid order timestamp");
  }
  return value;
}

function parseItem(value: unknown): OrderItem {
  if (!isRecord(value)) throw new Error("Invalid order item");
  return {
    productId: text(value.productId, true),
    sku: text(value.sku),
    name: text(value.name),
    image: text(value.image),
    manufacturerId: text(value.manufacturerId),
    manufacturer: text(value.manufacturer),
    quantity: integer(value.quantity, 1),
    unitPriceMinor: integer(value.unitPriceMinor, 0),
    lineTotalMinor: integer(value.lineTotalMinor, 0),
  };
}

export function parseOrder(body: unknown): Order {
  if (!isRecord(body) || !Array.isArray(body.items)) throw new Error("Invalid order response");
  if (body.comment !== undefined && typeof body.comment !== "string") throw new Error("Invalid order comment");
  const deliveredAt = timestamp(body.deliveredAt);
  return {
    id: text(body.id, true),
    number: text(body.number, true),
    status: text(body.status),
    statusLabel: text(body.statusLabel),
    items: body.items.map(parseItem),
    itemCount: integer(body.itemCount, 0),
    totalMinor: integer(body.totalMinor, 0),
    deliveryAddress: text(body.deliveryAddress),
    ...(typeof body.comment === "string" && body.comment ? { comment: body.comment } : {}),
    createdAt: timestamp(body.createdAt),
    deliveredAt: deliveredAt.startsWith("0001-01-01") ? null : deliveredAt,
  };
}
