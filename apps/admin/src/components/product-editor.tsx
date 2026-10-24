"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";

import { failureMessages, type ApiFailure } from "@/lib/admin-api";
import {
  emptyProductForm,
  errorsFromServer,
  fieldLabels,
  formFields,
  formFromProduct,
  payloadFromForm,
  sameForm,
  type FormErrors,
  type FormField,
  type ProductFormValues,
} from "@/lib/product-form";
import {
  archiveProduct,
  createProduct,
  getCatalogOptions,
  getProduct,
  restoreProduct,
  updateProduct,
  type AdminProduct,
  type CatalogOptions,
} from "@/lib/products-api";
import { useStaffApi } from "./use-staff-api";
import { useUnsavedChanges } from "./use-unsaved-changes";
import styles from "./products.module.css";

type Load = { status: "loading" } | { status: "ready" } | { status: "error"; failure: ApiFailure };
type Notice = { tone: "success" | "error"; text: string } | null;
// The action that met a newer revision. Nothing is retried or merged: the
// operator reloads the server version or leaves.
type Conflict = "save" | "archive" | "restore" | null;

export const conflictMessage = "Товар был изменён другим сотрудником. Ваши изменения не сохранены.";
export const archiveExplanation =
  "Товар исчезнет из каталога, поиска и подборок магазина: его нельзя будет купить или добавить в корзину. " +
  "Заказы, корзины и избранное покупателей сохранятся, а товар можно будет вернуть в продажу.";

const fieldId = (field: FormField) => `product-${field}`;
const errorId = (field: FormField) => `product-${field}-error`;

/**
 * Creates a product (no productId) or edits one. The form starts from the
 * server's product and keeps the revision it was loaded at; every write sends
 * that revision, and only an explicit reload replaces it.
 */
export function ProductEditor({ productId, created = false }: { productId?: string; created?: boolean }) {
  const router = useRouter();
  const { read, mutate } = useStaffApi();
  const [load, setLoad] = useState<Load>({ status: "loading" });
  const [options, setOptions] = useState<CatalogOptions | null>(null);
  const [product, setProduct] = useState<AdminProduct | null>(null);
  const [form, setForm] = useState<ProductFormValues>(emptyProductForm);
  const [baseline, setBaseline] = useState<ProductFormValues>(emptyProductForm);
  const [errors, setErrors] = useState<FormErrors>({});
  const [formMessages, setFormMessages] = useState<string[]>([]);
  const [notice, setNotice] = useState<Notice>(created ? { tone: "success", text: "Товар создан." } : null);
  const [conflict, setConflict] = useState<Conflict>(null);
  const [busy, setBusy] = useState<"save" | "archive" | "restore" | "reload" | null>(null);
  const [confirmArchive, setConfirmArchive] = useState(false);
  const summaryRef = useRef<HTMLDivElement>(null);
  const conflictRef = useRef<HTMLDivElement>(null);
  const confirmRef = useRef<HTMLHeadingElement>(null);

  const dirty = !sameForm(form, baseline);
  useUnsavedChanges(dirty);

  const adopt = useCallback((next: AdminProduct) => {
    const values = formFromProduct(next);
    setProduct(next);
    setForm(values);
    setBaseline(values);
    setErrors({});
    setFormMessages([]);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      const [optionResult, productResult] = await Promise.all([
        read(() => getCatalogOptions(controller.signal)),
        productId ? read(() => getProduct(productId, controller.signal)) : Promise.resolve(null),
      ]);
      if (controller.signal.aborted) return;
      if (!optionResult.ok) return setLoad({ status: "error", failure: optionResult.error });
      if (productResult && !productResult.ok) return setLoad({ status: "error", failure: productResult.error });
      setOptions(optionResult.data);
      if (productResult) adopt(productResult.data);
      setLoad({ status: "ready" });
    })();
    return () => controller.abort();
  }, [adopt, productId, read]);

  const set = <K extends FormField>(field: K, value: ProductFormValues[K]) => setForm((current) => ({ ...current, [field]: value }));

  function showFailure(failure: ApiFailure, action: Exclude<Conflict, null>) {
    if (failure.kind === "stale_revision") {
      setConflict(action);
      requestAnimationFrame(() => conflictRef.current?.focus());
      return;
    }
    if (failure.kind === "validation") {
      const mapped = errorsFromServer(failure.fields);
      setErrors(mapped.errors);
      setFormMessages(mapped.other);
      requestAnimationFrame(() => summaryRef.current?.focus());
      return;
    }
    if (failure.kind === "staff_changed") router.refresh();
    if (failure.kind !== "unauthorized") setNotice({ tone: "error", text: failureMessages[failure.kind] });
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || conflict) return;
    setNotice(null);
    const check = payloadFromForm(form);
    if (!check.ok) {
      setErrors(check.errors);
      setFormMessages([]);
      requestAnimationFrame(() => summaryRef.current?.focus());
      return;
    }
    setErrors({});
    setFormMessages([]);
    setBusy("save");
    const result = product
      ? await mutate((auth) => updateProduct(product.id, product.revision, check.payload, auth))
      : await mutate((auth) => createProduct(check.payload, auth));
    setBusy(null);
    if (!result.ok) return showFailure(result.error, "save");
    if (!product) {
      // The server assigned the ID; the edit page loads its authoritative state.
      setBaseline(form);
      router.replace(`/products/${encodeURIComponent(result.data.id)}?created=1`);
      return;
    }
    adopt(result.data);
    setNotice({ tone: "success", text: `Изменения сохранены. Ревизия ${result.data.revision}.` });
  }

  async function reloadServerVersion() {
    if (!product) return;
    setBusy("reload");
    const result = await read(() => getProduct(product.id));
    setBusy(null);
    if (!result.ok) {
      if (result.error.kind !== "unauthorized") setNotice({ tone: "error", text: failureMessages[result.error.kind] });
      return;
    }
    adopt(result.data);
    setConflict(null);
    setNotice({ tone: "success", text: `Загружена актуальная версия. Ревизия ${result.data.revision}.` });
  }

  async function changeArchive(archive: boolean) {
    if (!product || busy) return;
    setConfirmArchive(false);
    setNotice(null);
    setBusy(archive ? "archive" : "restore");
    const result = await mutate((auth) => (archive ? archiveProduct : restoreProduct)(product.id, product.revision, auth));
    setBusy(null);
    if (!result.ok) return showFailure(result.error, archive ? "archive" : "restore");
    adopt(result.data);
    setNotice({ tone: "success", text: archive ? `Товар перенесён в архив. Ревизия ${result.data.revision}.` : `Товар снова в продаже. Ревизия ${result.data.revision}.` });
  }

  const brands = useMemo(() => options?.brands.filter((brand) => brand.manufacturerId === form.manufacturerId) ?? [], [options, form.manufacturerId]);
  const categories = useMemo(() => categoryTree(options?.categories ?? []), [options]);

  if (load.status === "loading") {
    return (
      <p aria-live="polite" className={styles.muted} role="status">
        Загрузка…
      </p>
    );
  }
  if (load.status === "error") {
    return (
      <section className={styles.page}>
        <h1>{load.failure.kind === "not_found" ? "Товар не найден" : "Не удалось открыть товар"}</h1>
        <p className={styles.muted} role="alert">
          {failureMessages[load.failure.kind]}
        </p>
        <Link href="/products">К списку товаров</Link>
      </section>
    );
  }

  const errorEntries = formFields.filter((field) => errors[field]);
  const describedBy = (field: FormField, hint?: string) => [hint, errors[field] ? errorId(field) : undefined].filter(Boolean).join(" ") || undefined;
  const text = (field: FormField, extra: { required?: boolean; hint?: string; inputMode?: "decimal" | "numeric"; maxLength?: number; wide?: boolean } = {}) => (
    <Field error={errors[field]} field={field} hint={extra.hint} wide={extra.wide}>
      <input
        aria-describedby={describedBy(field, extra.hint ? `${fieldId(field)}-hint` : undefined)}
        aria-invalid={errors[field] ? true : undefined}
        aria-required={extra.required || undefined}
        id={fieldId(field)}
        inputMode={extra.inputMode}
        maxLength={extra.maxLength}
        onChange={(event) => set(field, event.target.value as never)}
        type="text"
        value={form[field] as string}
      />
    </Field>
  );
  const area = (field: FormField, hint?: string) => (
    <Field error={errors[field]} field={field} hint={hint}>
      <textarea
        aria-describedby={describedBy(field, hint ? `${fieldId(field)}-hint` : undefined)}
        aria-invalid={errors[field] ? true : undefined}
        id={fieldId(field)}
        onChange={(event) => set(field, event.target.value as never)}
        rows={field === "description" ? 5 : 3}
        value={form[field] as string}
      />
    </Field>
  );

  return (
    <section aria-labelledby="product-title" className={styles.page}>
      <p className={styles.breadcrumb}>
        <Link href="/products">Товары</Link>
      </p>
      <div className={styles.heading}>
        <h1 id="product-title">{product ? product.name : "Новый товар"}</h1>
        {product ? (
          <p className={product.archived ? styles.statusArchived : styles.statusActive}>{product.archived ? "В архиве" : "В продаже"}</p>
        ) : null}
      </div>
      {product ? (
        <dl className={styles.facts}>
          <div>
            <dt>ID</dt>
            <dd>{product.id}</dd>
          </div>
          <div>
            <dt>Ревизия</dt>
            <dd data-testid="product-revision">{product.revision}</dd>
          </div>
          <div>
            <dt>Обновлён</dt>
            <dd>{new Date(product.updatedAt).toLocaleString("ru-RU")}</dd>
          </div>
        </dl>
      ) : null}
      {product?.archived ? <p className={styles.archivedNote}>Товар в архиве: покупатели его не видят и не могут купить. Его можно редактировать.</p> : null}

      <div aria-live="polite" role="status">
        {notice?.tone === "success" ? <p className={styles.success}>{notice.text}</p> : null}
      </div>
      {notice?.tone === "error" ? (
        <p className={styles.alert} role="alert">
          {notice.text}
        </p>
      ) : null}

      {conflict ? (
        <div className={styles.conflict} ref={conflictRef} role="alert" tabIndex={-1}>
          <p>
            <strong>{conflictMessage}</strong>
          </p>
          <p>Ваши значения остались в форме: при необходимости скопируйте их, затем загрузите актуальную версию.</p>
          <div className={styles.actions}>
            <button className={styles.primaryButton} disabled={busy === "reload"} onClick={reloadServerVersion} type="button">
              {busy === "reload" ? "Загружаем…" : "Загрузить актуальную версию"}
            </button>
            <button
              className={styles.secondaryAction}
              onClick={() => {
                setBaseline(form);
                router.push("/products");
              }}
              type="button"
            >
              Отменить мои изменения и вернуться к списку
            </button>
          </div>
        </div>
      ) : null}

      {errorEntries.length || formMessages.length ? (
        <div aria-labelledby="product-errors-title" className={styles.summary} ref={summaryRef} role="alert" tabIndex={-1}>
          <h2 id="product-errors-title">Исправьте ошибки</h2>
          <ul>
            {errorEntries.map((field) => (
              <li key={field}>
                <a href={`#${fieldId(field)}`}>
                  {fieldLabels[field]}: {errors[field]}
                </a>
              </li>
            ))}
            {formMessages.map((message) => (
              <li key={message}>{message}</li>
            ))}
          </ul>
        </div>
      ) : null}

      <form className={styles.form} noValidate onSubmit={save}>
        <fieldset>
          <legend>Основное</legend>
          {text("name", { required: true, maxLength: 200, wide: true })}
          {text("shortName", { maxLength: 100 })}
          {text("slug", { required: true, hint: "Строчные латинские буквы, цифры и дефисы: chocolate-bar", maxLength: 120 })}
          {text("sku", { required: true, maxLength: 64 })}
          {area("description")}
        </fieldset>
        <fieldset>
          <legend>Цена и наличие</legend>
          {text("price", { required: true, inputMode: "decimal", hint: "В рублях: 199 или 199,90" })}
          {text("oldPrice", { inputMode: "decimal", hint: "Пусто — без скидки. Иначе больше текущей цены" })}
          {text("stock", { inputMode: "numeric" })}
          {text("minimumQuantity", { inputMode: "numeric" })}
          {text("quantityStep", { inputMode: "numeric", hint: "Количество в корзине: минимум плюс целое число шагов" })}
          {text("unit", { required: true, hint: "Например: плитка 90 г", maxLength: 50 })}
          {text("weightGrams", { inputMode: "numeric" })}
          {text("shelfLifeDays", { inputMode: "numeric" })}
        </fieldset>
        <fieldset>
          <legend>Каталог</legend>
          <Field error={errors.categoryId} field="categoryId">
            <select
              aria-describedby={describedBy("categoryId")}
              aria-invalid={errors.categoryId ? true : undefined}
              id={fieldId("categoryId")}
              onChange={(event) => set("categoryId", event.target.value)}
              value={form.categoryId}
            >
              <option value="">Выберите категорию</option>
              {categories.map(({ category, depth }) => (
                <option key={category.id} value={category.id}>
                  {"— ".repeat(depth) + category.name}
                </option>
              ))}
            </select>
          </Field>
          <Field error={errors.manufacturerId} field="manufacturerId">
            <select
              aria-describedby={describedBy("manufacturerId")}
              aria-invalid={errors.manufacturerId ? true : undefined}
              id={fieldId("manufacturerId")}
              onChange={(event) => {
                const manufacturerId = event.target.value;
                // A brand belongs to one manufacturer: never keep a foreign one.
                setForm((current) => ({
                  ...current,
                  manufacturerId,
                  brandId: options?.brands.some((brand) => brand.id === current.brandId && brand.manufacturerId === manufacturerId) ? current.brandId : "",
                }));
              }}
              value={form.manufacturerId}
            >
              <option value="">Выберите производителя</option>
              {options?.manufacturers.map((manufacturer) => (
                <option key={manufacturer.id} value={manufacturer.id}>
                  {manufacturer.name}
                </option>
              ))}
            </select>
          </Field>
          <Field error={errors.brandId} field="brandId" hint="Только бренды выбранного производителя">
            <select
              aria-describedby={describedBy("brandId", `${fieldId("brandId")}-hint`)}
              aria-invalid={errors.brandId ? true : undefined}
              disabled={!form.manufacturerId}
              id={fieldId("brandId")}
              onChange={(event) => set("brandId", event.target.value)}
              value={form.brandId}
            >
              <option value="">{form.manufacturerId ? "Выберите бренд" : "Сначала выберите производителя"}</option>
              {brands.map((brand) => (
                <option key={brand.id} value={brand.id}>
                  {brand.name}
                </option>
              ))}
            </select>
          </Field>
          <fieldset aria-describedby={errors.collectionIds ? errorId("collectionIds") : undefined} className={styles.checklist}>
            <legend>{fieldLabels.collectionIds}</legend>
            {options?.collections.length ? (
              options.collections.map((collection) => (
                <label key={collection.id}>
                  <input
                    checked={form.collectionIds.includes(collection.id)}
                    onChange={(event) =>
                      set(
                        "collectionIds",
                        event.target.checked ? [...form.collectionIds, collection.id] : form.collectionIds.filter((id) => id !== collection.id),
                      )
                    }
                    type="checkbox"
                  />
                  {collection.name}
                </label>
              ))
            ) : (
              <p className={styles.muted}>Подборок нет.</p>
            )}
            {errors.collectionIds ? (
              <p className={styles.fieldError} id={errorId("collectionIds")}>
                {errors.collectionIds}
              </p>
            ) : null}
          </fieldset>
          {text("series", { maxLength: 100 })}
          {area("tags", "По одному на строку")}
          {area("badges", "По одному на строку, например: Новинка")}
          <label className={styles.checkbox}>
            <input checked={form.featured} id={fieldId("featured")} onChange={(event) => set("featured", event.target.checked)} type="checkbox" />
            {fieldLabels.featured}
          </label>
        </fieldset>
        <fieldset>
          <legend>Состав</legend>
          {area("ingredients")}
          {area("nutrition")}
        </fieldset>
        {product ? <Media product={product} /> : null}
        <div className={styles.formActions}>
          <button className={styles.primaryButton} disabled={busy !== null || conflict !== null} type="submit">
            {busy === "save" ? "Сохраняем…" : product ? "Сохранить изменения" : "Создать товар"}
          </button>
          {dirty ? <span className={styles.muted}>Есть несохранённые изменения</span> : null}
        </div>
      </form>

      {product ? (
        <section aria-labelledby="product-availability" className={styles.availability}>
          <h2 id="product-availability">Продажа</h2>
          {product.archived ? (
            <>
              <p>Товар в архиве. Вернуть его в каталог магазина можно в любой момент.</p>
              <button className={styles.secondaryAction} disabled={busy !== null || conflict !== null || dirty} onClick={() => changeArchive(false)} type="button">
                {busy === "restore" ? "Восстанавливаем…" : "Вернуть в продажу"}
              </button>
            </>
          ) : confirmArchive ? (
            <div aria-labelledby="archive-confirm-title" className={styles.confirm} role="group">
              <h3 id="archive-confirm-title" ref={confirmRef} tabIndex={-1}>
                Перенести товар в архив?
              </h3>
              <p>{archiveExplanation}</p>
              <div className={styles.actions}>
                <button className={styles.dangerButton} onClick={() => changeArchive(true)} type="button">
                  Перенести в архив
                </button>
                <button className={styles.secondaryAction} onClick={() => setConfirmArchive(false)} type="button">
                  Отмена
                </button>
              </div>
            </div>
          ) : (
            <>
              <p>{archiveExplanation}</p>
              <button
                className={styles.secondaryAction}
                disabled={busy !== null || conflict !== null || dirty}
                onClick={() => {
                  setConfirmArchive(true);
                  requestAnimationFrame(() => confirmRef.current?.focus());
                }}
                type="button"
              >
                {busy === "archive" ? "Переносим…" : "Снять с продажи…"}
              </button>
            </>
          )}
          {dirty ? <p className={styles.muted}>Сначала сохраните или отмените изменения формы.</p> : null}
        </section>
      ) : null}
    </section>
  );
}

function Field({ field, error, hint, wide, children }: { field: FormField; error?: string; hint?: string; wide?: boolean; children: ReactNode }) {
  return (
    <div className={wide ? `${styles.field} ${styles.wide}` : styles.field}>
      <label htmlFor={fieldId(field)}>{fieldLabels[field]}</label>
      {children}
      {hint ? (
        <p className={styles.hint} id={`${fieldId(field)}-hint`}>
          {hint}
        </p>
      ) : null}
      {error ? (
        <p className={styles.fieldError} id={errorId(field)}>
          {error}
        </p>
      ) : null}
    </div>
  );
}

function Media({ product }: { product: AdminProduct }) {
  const paths = [product.image, ...product.gallery.filter((path) => path !== product.image)].filter(Boolean);
  return (
    <section aria-labelledby="product-media" className={styles.media}>
      <h2 id="product-media">Изображения</h2>
      {paths.length ? (
        <ul>
          {paths.map((path) => (
            <li key={path}>
              <code>{path}</code>
            </li>
          ))}
        </ul>
      ) : (
        <p>Изображений нет.</p>
      )}
      <p className={styles.muted}>Только просмотр: управление изображениями будет добавлено отдельно.</p>
    </section>
  );
}

function categoryTree(categories: CatalogOptions["categories"]) {
  const result: Array<{ category: CatalogOptions["categories"][number]; depth: number }> = [];
  const known = new Set(categories.map((category) => category.id));
  const visit = (parentId: string | undefined, depth: number) => {
    for (const category of categories) {
      const parent = category.parentId && known.has(category.parentId) ? category.parentId : undefined;
      if (parent === parentId && depth < 5) {
        result.push({ category, depth });
        visit(category.id, depth + 1);
      }
    }
  };
  visit(undefined, 0);
  return result;
}
