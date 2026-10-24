// Dictionary page state shared by server pages and client components: titles,
// the URL status filter and links. Plain module, so server pages can call it.

import type { DictionaryKind, DictionaryStatus } from "./dictionaries-api";

export const dictionaryTitles: Record<DictionaryKind, { title: string; create: string; empty: string }> = {
  categories: { title: "Категории", create: "Добавить категорию", empty: "Категорий нет." },
  manufacturers: { title: "Производители", create: "Добавить производителя", empty: "Производителей нет." },
  brands: { title: "Бренды", create: "Добавить бренд", empty: "Брендов нет." },
  collections: { title: "Подборки", create: "Добавить подборку", empty: "Подборок нет." },
};

/** The status badge, in the dictionary's grammatical gender. */
export const activeLabel: Record<DictionaryKind, string> = { categories: "Активна", manufacturers: "Активен", brands: "Активен", collections: "Активна" };

export const dictionaryStatusLabels: Record<DictionaryStatus, string> = { active: "Активные", archived: "В архиве", all: "Все" };
export const dictionaryStatuses = Object.keys(dictionaryStatusLabels) as DictionaryStatus[];

/** The list state lives in the URL; an unknown status falls back to active. */
export function parseDictionaryStatus(value: string | string[] | undefined): DictionaryStatus {
  const status = (Array.isArray(value) ? value[0] : value) as DictionaryStatus;
  return dictionaryStatuses.includes(status) ? status : "active";
}

export const dictionaryListHref = (kind: DictionaryKind, status: DictionaryStatus) =>
  status === "active" ? `/dictionaries/${kind}` : `/dictionaries/${kind}?status=${status}`;

export const entryHref = (kind: DictionaryKind, id: string) => `/dictionaries/${kind}/${encodeURIComponent(id)}`;
