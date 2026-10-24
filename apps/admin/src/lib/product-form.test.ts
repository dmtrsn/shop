import { describe, expect, it } from "vitest";

import { product } from "@/test/api-mock";
import { emptyProductForm, errorsFromServer, formFromProduct, payloadFromForm, sameForm } from "./product-form";

describe("product form model", () => {
  it("turns a loaded product into editable text and back into the exact B1 document", () => {
    const values = formFromProduct(product());
    expect(values.price).toBe("489,00");
    expect(values.oldPrice).toBe("549,00");
    expect(values.tags).toBe("веган");
    const check = payloadFromForm(values);
    expect(check).toEqual({
      ok: true,
      payload: {
        slug: "dark-bar", sku: "CHA-701", name: "Тёмный шоколад", shortName: "Тёмный", description: "Описание", priceMinor: 48900,
        oldPriceMinor: 54900, unit: "плитка 90 г", weightGrams: 90, categoryId: "cat-bars", manufacturerId: "m-choco", brandId: "b-noir",
        collectionIds: ["col-best"], series: "", tags: ["веган"], badges: ["Хит"], ingredients: "", nutrition: "", shelfLifeDays: 180,
        stock: 24, minimumQuantity: 1, quantityStep: 1, featured: true,
      },
    });
    expect(Object.keys((check as { payload: object }).payload)).toHaveLength(23);
    expect(formFromProduct(product({ oldPriceMinor: 0 })).oldPrice).toBe("");
  });

  it("reads lists one per line and an empty old price as none", () => {
    const check = payloadFromForm({ ...formFromProduct(product()), tags: " a \n\n b ", badges: "", oldPrice: " " });
    expect(check.ok && check.payload).toMatchObject({ tags: ["a", "b"], badges: [], oldPriceMinor: 0 });
  });

  it("never rounds or clamps whole numbers or amounts", () => {
    const base = formFromProduct(product());
    for (const [field, value] of [["stock", "-1"], ["stock", "2.5"], ["minimumQuantity", "0"], ["quantityStep", "0"], ["quantityStep", "1e2"], ["weightGrams", "ten"]] as const) {
      const check = payloadFromForm({ ...base, [field]: value });
      expect(check.ok ? null : Object.keys(check.errors)).toEqual([field]);
    }
    for (const [field, value] of [["price", "199,999"], ["price", "0"], ["price", ""], ["oldPrice", "abc"]] as const) {
      const check = payloadFromForm({ ...base, [field]: value });
      expect(check.ok ? null : Object.keys(check.errors)).toEqual([field]);
    }
  });

  it("requires the identity fields and references of a new product", () => {
    const check = payloadFromForm(emptyProductForm);
    expect(check.ok ? [] : Object.keys(check.errors)).toEqual(["name", "slug", "sku", "unit", "categoryId", "manufacturerId", "brandId", "price"]);
  });

  it("maps Go's field errors to controls and keeps the rest", () => {
    expect(
      errorsFromServer([
        { field: "priceMinor", message: "Цена" },
        { field: "slug", message: "Занят" },
        { field: "brandId", message: "Чужой" },
        { field: "body", message: "Некорректный JSON" },
      ]),
    ).toEqual({ errors: { price: "Цена", slug: "Занят", brandId: "Чужой" }, other: ["Некорректный JSON"] });
  });

  it("compares forms by value", () => {
    const values = formFromProduct(product());
    expect(sameForm(values, { ...values, collectionIds: [...values.collectionIds] })).toBe(true);
    expect(sameForm(values, { ...values, stock: "25" })).toBe(false);
  });
});
