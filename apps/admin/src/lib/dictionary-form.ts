// The dictionary form model: what the operator edits (strings, as typed), how
// each dictionary's fields become its exact write document, and how Go's
// field errors map back. Client checks only catch typos early; Go validates
// every write.

import type { FieldError } from "./admin-api";
import {
  collectionAccents,
  type AdminBrand,
  type AdminCategory,
  type AdminCollection,
  type AdminManufacturer,
  type CollectionAccent,
  type DictionaryEntry,
  type DictionaryKind,
  type DictionaryPayload,
} from "./dictionaries-api";

export type DictionaryFormValues = {
  name: string;
  slug: string;
  parentId: string;
  manufacturerId: string;
  country: string;
  icon: string;
  accent: string;
  // Calendar days (YYYY-MM-DD) in Moscow time; empty is open-ended.
  startsAt: string;
  endsAt: string;
  sortOrder: string;
  description: string;
};

export type DictionaryField = keyof DictionaryFormValues;
export type DictionaryFormErrors = Partial<Record<DictionaryField, string>>;

export const dictionaryFieldLabels: Record<DictionaryField, string> = {
  name: "Название",
  slug: "Slug (адрес в каталоге)",
  parentId: "Родительская категория",
  manufacturerId: "Производитель",
  country: "Страна",
  icon: "Иконка",
  accent: "Акцент",
  startsAt: "Показывать с",
  endsAt: "Показывать по",
  sortOrder: "Порядок",
  description: "Описание",
};

/** Each dictionary's fields, in form order. */
export const dictionaryFields: Record<DictionaryKind, DictionaryField[]> = {
  categories: ["name", "slug", "parentId", "icon", "sortOrder", "description"],
  manufacturers: ["name", "slug", "country", "sortOrder", "description"],
  brands: ["name", "slug", "manufacturerId", "sortOrder", "description"],
  collections: ["name", "slug", "accent", "startsAt", "endsAt", "sortOrder", "description"],
};

export const accentLabels: Record<CollectionAccent, string> = { lime: "Лайм", cocoa: "Какао", coral: "Коралл", sky: "Небо" };

export const emptyDictionaryForm: DictionaryFormValues = {
  name: "", slug: "", parentId: "", manufacturerId: "", country: "", icon: "", accent: "cocoa",
  startsAt: "", endsAt: "", sortOrder: "0", description: "",
};

// Russia keeps UTC+3 all year; collection days are Moscow days.
const MOSCOW = "+03:00";
const MOSCOW_OFFSET_MS = 3 * 60 * 60 * 1000;

export function moscowDay(iso: string | undefined): string {
  if (!iso) return "";
  const time = Date.parse(iso);
  return Number.isNaN(time) ? "" : new Date(time + MOSCOW_OFFSET_MS).toISOString().slice(0, 10);
}

export function formFromEntry<K extends DictionaryKind>(kind: K, entry: DictionaryEntry[K]): DictionaryFormValues {
  const values: DictionaryFormValues = { ...emptyDictionaryForm, name: entry.name, slug: entry.slug, sortOrder: String(entry.sortOrder), description: entry.description };
  switch (kind) {
    case "categories": {
      const category = entry as AdminCategory;
      return { ...values, parentId: category.parentId ?? "", icon: category.icon };
    }
    case "manufacturers":
      return { ...values, country: (entry as AdminManufacturer).country };
    case "brands":
      return { ...values, manufacturerId: (entry as AdminBrand).manufacturer.id };
    default: {
      const collection = entry as AdminCollection;
      return { ...values, accent: collection.accent, startsAt: moscowDay(collection.startsAt), endsAt: moscowDay(collection.endsAt) };
    }
  }
}

export function sameDictionaryForm(a: DictionaryFormValues, b: DictionaryFormValues): boolean {
  return (Object.keys(a) as DictionaryField[]).every((field) => a[field] === b[field]);
}

const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const dayPattern = /^\d{4}-\d{2}-\d{2}$/;

export type DictionaryCheck<K extends DictionaryKind> = { ok: true; payload: DictionaryPayload[K] } | { ok: false; errors: DictionaryFormErrors };

/** Builds the write document of one dictionary, or the fields to fix. */
export function payloadFromDictionaryForm<K extends DictionaryKind>(kind: K, form: DictionaryFormValues): DictionaryCheck<K> {
  const errors: DictionaryFormErrors = {};
  const name = form.name.trim();
  const slug = form.slug.trim();
  if (!name) errors.name = "Укажите название";
  else if (name.length > 120) errors.name = "Не длиннее 120 символов";
  if (!slugPattern.test(slug) || slug.length > 120) errors.slug = "Строчные латинские буквы, цифры и дефисы между ними";
  const sortOrder = Number(form.sortOrder.trim());
  if (!/^\d+$/.test(form.sortOrder.trim()) || sortOrder > 100_000) errors.sortOrder = "Целое число от 0 до 100000";
  const description = form.description.trim();
  if (description.length > 2000) errors.description = "Не длиннее 2000 символов";
  const common = { slug, name, description, sortOrder };

  let payload: DictionaryPayload[DictionaryKind];
  switch (kind) {
    case "categories": {
      const icon = form.icon.trim();
      if (icon && (!slugPattern.test(icon) || icon.length > 40)) errors.icon = "Латинские буквы, цифры и дефисы, до 40 символов";
      payload = { ...common, parentId: form.parentId, icon };
      break;
    }
    case "manufacturers":
      if (form.country.trim().length > 60) errors.country = "Не длиннее 60 символов";
      payload = { ...common, country: form.country.trim() };
      break;
    case "brands":
      if (!form.manufacturerId) errors.manufacturerId = "Выберите производителя";
      payload = { ...common, manufacturerId: form.manufacturerId };
      break;
    default: {
      const accent = form.accent as CollectionAccent;
      if (!collectionAccents.includes(accent)) errors.accent = "Выберите акцент";
      for (const field of ["startsAt", "endsAt"] as const) {
        if (form[field] && !dayPattern.test(form[field])) errors[field] = "Дата в формате ГГГГ-ММ-ДД";
      }
      if (form.startsAt && form.endsAt && !errors.startsAt && !errors.endsAt && form.endsAt < form.startsAt) errors.endsAt = "Не раньше даты начала";
      payload = {
        ...common,
        accent,
        // The whole first and last day, Moscow time.
        startsAt: form.startsAt ? `${form.startsAt}T00:00:00${MOSCOW}` : null,
        endsAt: form.endsAt ? `${form.endsAt}T23:59:59${MOSCOW}` : null,
      };
    }
  }
  return Object.keys(errors).length ? { ok: false, errors } : { ok: true, payload: payload as DictionaryPayload[K] };
}

/** Go's field errors: known fields go next to their input, the rest to the summary. */
export function dictionaryErrorsFromServer(kind: DictionaryKind, fields: FieldError[]): { errors: DictionaryFormErrors; other: string[] } {
  const errors: DictionaryFormErrors = {};
  const other: string[] = [];
  for (const { field, message } of fields) {
    if ((dictionaryFields[kind] as string[]).includes(field)) errors[field as DictionaryField] ??= message;
    else other.push(message);
  }
  return { errors, other };
}
