import type { RepeatBody, RepeatLine, RepeatPreview } from "./api/repeat";

export type RepeatDraft = { include: boolean; quantity: string };
export type RepeatDrafts = Record<number, RepeatDraft>;
export const canRepeat = (line: RepeatLine) => Boolean(line.product && (line.status === "available" || line.status === "limited"));
export function additionError(line: RepeatLine, draft: string): string {
  const quantity = Number(draft);
  if (!/^\d+$/.test(draft) || !Number.isSafeInteger(quantity) || quantity > 2147483647) return "Введите целое количество.";
  if (quantity === 0) return "Чтобы не добавлять товар, исключите его из повтора";
  const p = line.product;
  if (!p || !canRepeat(line)) return "Товар недоступен для повтора.";
  const final = line.existingCartQuantity + quantity;
  if (!Number.isSafeInteger(final) || final > 2147483647 || final > p.stock) return `В корзине может быть не больше ${p.stock}. Уменьшите добавление.`;
  if (final < p.minimumQuantity || (final - p.minimumQuantity) % p.quantityStep !== 0) return `Итог в корзине: минимум ${p.minimumQuantity}, затем шаг ${p.quantityStep}.`;
  return "";
}
function identity(line: RepeatLine) {
  return JSON.stringify([line.sourcePosition, line.productId, line.sku, line.name, line.image, line.requestedQuantity, line.originalUnitPriceMinor]);
}
export function repeatDrafts(preview: RepeatPreview, old?: { preview: RepeatPreview; drafts: RepeatDrafts }): RepeatDrafts {
  return Object.fromEntries(preview.items.map((line) => {
    const previous = old?.preview.items.find((row) => row.sourcePosition === line.sourcePosition);
    const kept = previous && identity(previous) === identity(line) ? old?.drafts[line.sourcePosition] : undefined;
    const quantity = String(line.suggestedQuantity || line.proposedQuantity || "");
    return [line.sourcePosition, kept ? { ...kept, include: canRepeat(line) && kept.include }
      : { quantity, include: canRepeat(line) && line.suggestedQuantity > 0 && !additionError(line, quantity) }];
  }));
}
export function repeatBody(preview: RepeatPreview, drafts: RepeatDrafts): RepeatBody {
  const items = preview.items.filter((row) => drafts[row.sourcePosition]?.include).map((row) => {
    const p = row.product;
    if (!p || additionError(row, drafts[row.sourcePosition].quantity)) throw new Error("Invalid repeat review");
    return Object.freeze({ sourcePosition: row.sourcePosition, productId: p.id, quantity: Number(drafts[row.sourcePosition].quantity),
      expectedExistingCartQuantity: row.existingCartQuantity, expectedUnitPriceMinor: p.priceMinor,
      expectedCurrency: p.currency, expectedMinimumQuantity: p.minimumQuantity, expectedQuantityStep: p.quantityStep });
  });
  if (!items.length) throw new Error("Empty repeat review");
  return Object.freeze({ items: Object.freeze(items) });
}
