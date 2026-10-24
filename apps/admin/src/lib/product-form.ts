// The product form model: what the operator edits (strings, as typed), how it
// becomes the exact B1 write document, and how Go's field errors map back.
// Client checks only catch typos early; Go validates every write.

import { parseRubles, roublesInput } from "./money";
import type { FieldError } from "./admin-api";
import type { AdminProduct, ProductPayload } from "./products-api";

export type ProductFormValues = {
  name: string;
  shortName: string;
  slug: string;
  sku: string;
  description: string;
  price: string;
  oldPrice: string;
  stock: string;
  minimumQuantity: string;
  quantityStep: string;
  unit: string;
  weightGrams: string;
  categoryId: string;
  manufacturerId: string;
  brandId: string;
  collectionIds: string[];
  series: string;
  // One label per line.
  tags: string;
  badges: string;
  featured: boolean;
  ingredients: string;
  nutrition: string;
  shelfLifeDays: string;
};

export type FormField = keyof ProductFormValues;
export type FormErrors = Partial<Record<FormField, string>>;

export const fieldLabels: Record<FormField, string> = {
  name: "Название",
  shortName: "Короткое название",
  slug: "Slug (адрес страницы)",
  sku: "Артикул",
  description: "Описание",
  price: "Цена, ₽",
  oldPrice: "Старая цена, ₽",
  stock: "Остаток",
  minimumQuantity: "Минимальное количество",
  quantityStep: "Шаг количества",
  unit: "Единица продажи",
  weightGrams: "Вес, г",
  categoryId: "Категория",
  manufacturerId: "Производитель",
  brandId: "Бренд",
  collectionIds: "Подборки",
  series: "Серия",
  tags: "Теги",
  badges: "Бейджи",
  featured: "Рекомендуемый товар",
  ingredients: "Состав",
  nutrition: "Пищевая ценность",
  shelfLifeDays: "Срок годности, дней",
};

// Field order of the form, for the error summary and first-error focus.
export const formFields = Object.keys(fieldLabels) as FormField[];

export const emptyProductForm: ProductFormValues = {
  name: "", shortName: "", slug: "", sku: "", description: "", price: "", oldPrice: "", stock: "0",
  minimumQuantity: "1", quantityStep: "1", unit: "", weightGrams: "0", categoryId: "", manufacturerId: "",
  brandId: "", collectionIds: [], series: "", tags: "", badges: "", featured: false, ingredients: "",
  nutrition: "", shelfLifeDays: "0",
};

export function formFromProduct(product: AdminProduct): ProductFormValues {
  return {
    name: product.name, shortName: product.shortName, slug: product.slug, sku: product.sku, description: product.description,
    price: roublesInput(product.priceMinor), oldPrice: product.oldPriceMinor ? roublesInput(product.oldPriceMinor) : "",
    stock: String(product.stock), minimumQuantity: String(product.minimumQuantity), quantityStep: String(product.quantityStep),
    unit: product.unit, weightGrams: String(product.weightGrams), categoryId: product.category.id,
    manufacturerId: product.manufacturer.id, brandId: product.brand.id, collectionIds: product.collections.map((c) => c.id),
    series: product.series, tags: product.tags.join("\n"), badges: product.badges.join("\n"), featured: product.featured,
    ingredients: product.ingredients, nutrition: product.nutrition, shelfLifeDays: String(product.shelfLifeDays),
  };
}

export function sameForm(a: ProductFormValues, b: ProductFormValues): boolean {
  return formFields.every((field) => JSON.stringify(a[field]) === JSON.stringify(b[field]));
}

const lines = (value: string) => value.split("\n").map((line) => line.trim()).filter(Boolean);

// Whole non-negative numbers exactly as typed: no signs, decimals or rounding.
function parseWhole(value: string): number | null {
  const text = value.trim();
  if (!/^\d+$/.test(text)) return null;
  const number = Number(text);
  return Number.isSafeInteger(number) ? number : null;
}

const moneyMessages = { empty: "Укажите цену", format: "Сумма в рублях: например 199 или 199,90", range: "Слишком большая сумма" } as const;

export type FormCheck = { ok: true; payload: ProductPayload } | { ok: false; errors: FormErrors };

/** Builds the B1 document, or the fields the operator must fix first. */
export function payloadFromForm(values: ProductFormValues): FormCheck {
  const errors: FormErrors = {};
  for (const field of ["name", "slug", "sku", "unit"] as const) {
    if (!values[field].trim()) errors[field] = "Обязательное поле";
  }
  for (const [field, message] of [["categoryId", "Выберите категорию"], ["manufacturerId", "Выберите производителя"], ["brandId", "Выберите бренд производителя"]] as const) {
    if (!values[field]) errors[field] = message;
  }
  const price = parseRubles(values.price);
  if (!price.ok) errors.price = moneyMessages[price.reason];
  else if (price.minor < 1) errors.price = "Цена должна быть больше нуля";
  const oldPrice = values.oldPrice.trim() ? parseRubles(values.oldPrice) : ({ ok: true, minor: 0 } as const);
  if (!oldPrice.ok) errors.oldPrice = moneyMessages[oldPrice.reason];
  const wholes: Partial<Record<FormField, number>> = {};
  for (const [field, minimum] of [["stock", 0], ["minimumQuantity", 1], ["quantityStep", 1], ["weightGrams", 0], ["shelfLifeDays", 0]] as const) {
    const number = parseWhole(values[field]);
    if (number === null) errors[field] = "Целое число без знака";
    else if (number < minimum) errors[field] = `Не меньше ${minimum}`;
    else wholes[field] = number;
  }
  if (Object.keys(errors).length > 0 || !price.ok || !oldPrice.ok) return { ok: false, errors };
  return {
    ok: true,
    payload: {
      slug: values.slug.trim(), sku: values.sku.trim(), name: values.name.trim(), shortName: values.shortName.trim(),
      description: values.description, priceMinor: price.minor, oldPriceMinor: oldPrice.minor, unit: values.unit.trim(),
      weightGrams: wholes.weightGrams!, categoryId: values.categoryId, manufacturerId: values.manufacturerId, brandId: values.brandId,
      collectionIds: values.collectionIds, series: values.series.trim(), tags: lines(values.tags), badges: lines(values.badges),
      ingredients: values.ingredients, nutrition: values.nutrition, shelfLifeDays: wholes.shelfLifeDays!, stock: wholes.stock!,
      minimumQuantity: wholes.minimumQuantity!, quantityStep: wholes.quantityStep!, featured: values.featured,
    },
  };
}

const serverFields: Record<string, FormField> = {
  ...Object.fromEntries(formFields.map((field) => [field, field])),
  priceMinor: "price",
  oldPriceMinor: "oldPrice",
};

/** Maps Go's field errors to controls; the rest are form-level messages. */
export function errorsFromServer(fields: FieldError[]): { errors: FormErrors; other: string[] } {
  const errors: FormErrors = {};
  const other: string[] = [];
  for (const { field, message } of fields) {
    const target = serverFields[field];
    if (target) errors[target] = errors[target] ? `${errors[target]} ${message}` : message;
    else other.push(message);
  }
  return { errors, other };
}
