import { describe, expect, it } from "vitest";

import type { Manufacturer } from "./api/catalog";
import {
  groupManufacturers,
  manufacturerGroupKey,
  producerCatalogUrl,
} from "./producer-navigation";

function manufacturer(name: string, slug: string): Manufacturer {
  return { id: slug, slug, name, country: "", productCount: 1 };
}

describe("producer navigation", () => {
  it("groups Latin, Cyrillic, Ё and other names without conflating them", () => {
    const groups = groupManufacturers([
      manufacturer("Белёвские сладости", "belev"),
      manufacturer("Caffarel", "caffarel"),
      manufacturer("Ёлочка", "yolochka"),
      manufacturer("7 конфет", "seven"),
      manufacturer("Choco Atelier", "choco"),
    ]);

    expect(groups.map((group) => group.key)).toEqual(["C", "Б", "Ё", "#"]);
    expect(groups[0].manufacturers.map((item) => item.name)).toEqual(["Caffarel", "Choco Atelier"]);
    expect(manufacturerGroupKey("  Белёвские сладости")).toBe("Б");
  });

  it("uses the catalog URL contract for manufacturer and brand", () => {
    expect(producerCatalogUrl({
      manufacturer: "choco-atelier",
      brand: "atelier-noir",
    })).toBe("/catalog?manufacturer=choco-atelier&brand=atelier-noir");
  });
});
