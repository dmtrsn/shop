import { describe, expect, it } from "vitest";
import { favoriteProduct } from "@/test/favorites-fixtures";
import { repeatLine, repeatPreview } from "@/test/repeat-fixtures";
import { additionError, repeatBody, repeatDrafts } from "./repeat-review";

describe("addition review", () => {
  it("validates final Cart quantity against minimum, step and capacity without clamping", () => {
    const line = repeatLine(1, "p01", { existingCartQuantity: 3, product: favoriteProduct("p01", { minimumQuantity: 4, quantityStep: 2, stock: 10 }) });
    expect(additionError(line, "1")).toBe("");
    expect(additionError(line, "3")).toBe("");
    expect(additionError(line, "2")).toMatch(/шаг/);
    expect(additionError(line, "0")).toMatch(/исключите/);
    for (const invalid of ["", "x", "-1", "1.2", "9007199254740992", "8"]) expect(additionError(line, invalid)).not.toBe("");
  });
  it("proposed-only requires deliberate inclusion, unavailable cannot be selected", () => {
    const preview = repeatPreview(repeatLine(1, "p01", { suggestedQuantity: 0, proposedQuantity: 4 }), repeatLine(5, "p02", { status: "unavailable" }));
    expect(repeatDrafts(preview)).toEqual({ 1: { include: false, quantity: "4" }, 5: { include: false, quantity: "2" } });
    expect(() => repeatBody(preview, repeatDrafts(preview))).toThrow();
  });
  it("omits exclusions, retains duplicate source lines and freezes all expectations", () => {
    const preview = repeatPreview(repeatLine(1), repeatLine(5), repeatLine(8, "p02"));
    const drafts = repeatDrafts(preview); drafts[8].include = false; drafts[5].quantity = "4";
    const body = repeatBody(preview, drafts);
    expect(body).toEqual({ items: [1, 5].map((sourcePosition) => ({ sourcePosition, productId: "p01", quantity: sourcePosition === 1 ? 2 : 4,
      expectedExistingCartQuantity: 1, expectedUnitPriceMinor: 48900, expectedCurrency: "RUB", expectedMinimumQuantity: 1, expectedQuantityStep: 1 })) });
    expect(Object.isFrozen(body.items[0])).toBe(true);
  });
  it("preserves drafts only for the same historical identity; never includes now unavailable rows", () => {
    const preview = repeatPreview(repeatLine(1), repeatLine(5), repeatLine(8));
    const drafts = repeatDrafts(preview); drafts[1] = { include: false, quantity: "bad" }; drafts[5].quantity = "7";
    const next = repeatPreview(repeatLine(1, "p01", { existingCartQuantity: 4 }), repeatLine(5, "other"), repeatLine(8, "p01", { status: "unavailable" }));
    expect(repeatDrafts(next, { preview, drafts })).toEqual({ 1: { include: false, quantity: "bad" }, 5: { include: true, quantity: "2" }, 8: { include: false, quantity: "2" } });
  });
});
