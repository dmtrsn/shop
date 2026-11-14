import { describe, expect, it } from "vitest";
import { adjustQuantity, checkQuantity, compareManufacturers, compareProducts, manufacturerSortKey, matchesManufacturer, matchesProduct } from "./quick-order";

describe("Fast Order search and ordering", () => {
  it("searches official manufacturer names without mutating them", () => {
    const m = { id: "1", name: '  ИП «Сопкалов К.А.»  ' };
    expect(matchesManufacturer(m, "сОп")).toBe(true);
    expect(matchesManufacturer(m, "ип  сопкалов")).toBe(true);
    expect(matchesManufacturer({ name: 'ООО «Ёлка»' }, 'ооо "елка"')).toBe(true);
    expect(matchesManufacturer(m, "соп нет")).toBe(false);
    expect(m.name).toBe('  ИП «Сопкалов К.А.»  ');
  });
  it("removes only a legal form token and has deterministic ties", () => {
    expect(manufacturerSortKey(' ООО «Ёлка» ')).toBe("елка");
    expect(manufacturerSortKey("ИП Сопкалов")).toBe("сопкалов");
    expect(manufacturerSortKey("АО Альфа")).toBe("альфа");
    expect(manufacturerSortKey("Аорта")).toBe("аорта");
    expect(manufacturerSortKey(" ИП «» ")).toBe("ип");
    const list = [{ id: "b", name: "ООО Елка" }, { id: "a", name: "ООО Ёлка" }, { id: "c", name: "ИП Елка" }];
    expect([...list].sort(compareManufacturers).map((m) => m.id)).toEqual(["c", "a", "b"]);
    expect(list[1].name).toBe("ООО Ёлка");
    expect(compareProducts({ id: "a", name: "Ёлка" }, { id: "b", name: "елка" })).toBeLessThan(0);
  });
  it("matches all name fragments in any order or a compact SKU", () => {
    const p = { name: "Сладкая шалость с персиком 2,5 кг — Ёлка", sku: "CHA-701" };
    for (const q of ["шалость", "шало", "персик шалость", "шалость персик", "елка", "2 5", "  СЛАДКАЯ   елка ", "", "cha-701", "701"]) expect(matchesProduct(p, q)).toBe(true);
    for (const q of ["шаласт", "персик 701", "CHA 701", "неизвестно"]) expect(matchesProduct(p, q)).toBe(false);
  });
});

describe("quantity drafts", () => {
  const rules = { minimumQuantity: 2, quantityStep: 3, stock: 11 };
  it("classifies drafts with the offset step and preserves rule details", () => {
    for (const [draft, state] of [["", "empty"], [" ", "empty"], ["0", "below-minimum"], ["-1", "below-minimum"], ["1.5", "invalid-integer"], ["abc", "invalid-integer"], ["1", "below-minimum"], ["2", "valid"], ["5", "valid"], ["6", "invalid-step"], ["11", "valid"], ["14", "above-stock"]]) {
      expect(checkQuantity(draft, rules)).toMatchObject({ state, rules, purchasable: true });
    }
    expect(checkQuantity("6", rules).suggestion).toBe(8);
    expect(checkQuantity("13", rules).suggestion).toBe(11);
    expect(checkQuantity("5", { minimumQuantity: 3, quantityStep: 3, stock: 10 }).suggestion).toBe(6);
    expect(checkQuantity("4", { minimumQuantity: 1, quantityStep: 1, stock: 4 }).state).toBe("valid");
    expect(checkQuantity("5", { minimumQuantity: 1, quantityStep: 1, stock: 4 })).toMatchObject({ state: "above-stock", suggestion: 4 });
    expect(checkQuantity("2", { ...rules, stock: 1 })).toMatchObject({ state: "above-stock", purchasable: false });
    expect(checkQuantity("2", { ...rules, stock: 1 }).suggestion).toBeUndefined();
    expect(checkQuantity("", { ...rules, stock: 0 }).purchasable).toBe(false);
  });
  it("adjusts only blank or valid drafts", () => {
    expect(adjustQuantity("", 1, rules)).toBe(2);
    expect(adjustQuantity("", -1, rules)).toBeUndefined();
    expect(adjustQuantity("5", 1, rules)).toBe(8);
    expect(adjustQuantity("5", -1, rules)).toBe(2);
    expect(adjustQuantity("2", -1, rules)).toBeUndefined();
    expect(adjustQuantity("11", 1, rules)).toBeUndefined();
    expect(adjustQuantity("6", 1, rules)).toBeUndefined();
    expect(adjustQuantity("", 1, { ...rules, stock: 1 })).toBeUndefined();
  });
});
