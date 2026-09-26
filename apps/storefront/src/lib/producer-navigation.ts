import type { Manufacturer } from "./api/catalog";
import { catalogUrl, parseCatalogState } from "./catalog-state";

export type ManufacturerGroup = {
  key: string;
  id: string;
  manufacturers: Manufacturer[];
};

const nameCollator = new Intl.Collator("ru", {
  numeric: true,
  sensitivity: "base",
});

export function manufacturerGroupKey(name: string): string {
  const first = Array.from(name.trim().normalize("NFKC"))[0]?.toLocaleUpperCase("ru");
  if (first && (/^[A-Z]$/u.test(first) || /^[А-ЯЁ]$/u.test(first))) return first;
  return "#";
}

function groupRank(key: string): number {
  if (/^[A-Z]$/u.test(key)) return 0;
  if (/^[А-ЯЁ]$/u.test(key)) return 1;
  return 2;
}

export function groupManufacturers(
  manufacturers: Manufacturer[],
): ManufacturerGroup[] {
  const grouped = new Map<string, Manufacturer[]>();
  for (const manufacturer of manufacturers) {
    const key = manufacturerGroupKey(manufacturer.name);
    grouped.set(key, [...(grouped.get(key) ?? []), manufacturer]);
  }

  return Array.from(grouped, ([key, items]) => ({
    key,
    id: key === "#" ? "manufacturer-group-other" : `manufacturer-group-${key}`,
    manufacturers: [...items].sort((left, right) =>
      nameCollator.compare(left.name, right.name),
    ),
  })).sort((left, right) => {
    const rank = groupRank(left.key) - groupRank(right.key);
    return rank || nameCollator.compare(left.key, right.key);
  });
}

export function producerCatalogUrl({
  manufacturer,
  brand,
}: {
  manufacturer: string;
  brand?: string;
}): string {
  return catalogUrl(parseCatalogState({}), { manufacturer, brand });
}
