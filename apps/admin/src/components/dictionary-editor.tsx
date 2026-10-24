"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";

import { failureMessages, type ApiFailure, type References } from "@/lib/admin-api";
import {
  collectionAccents,
  createDictionaryEntry,
  getDictionaryEntry,
  listDictionary,
  setDictionaryEntryArchived,
  updateDictionaryEntry,
  type AdminBrand,
  type AdminCategory,
  type AdminCollection,
  type AdminManufacturer,
  type DictionaryEntry,
  type DictionaryKind,
} from "@/lib/dictionaries-api";
import {
  accentLabels,
  dictionaryErrorsFromServer,
  dictionaryFieldLabels,
  dictionaryFields,
  emptyDictionaryForm,
  formFromEntry,
  payloadFromDictionaryForm,
  sameDictionaryForm,
  type DictionaryField,
  type DictionaryFormErrors,
  type DictionaryFormValues,
} from "@/lib/dictionary-form";
import { activeLabel, dictionaryTitles, entryHref } from "@/lib/dictionary-query";
import { DictionaryTabs } from "./dictionary-list";
import { useStaffApi } from "./use-staff-api";
import { useUnsavedChanges } from "./use-unsaved-changes";
import styles from "./admin-pages.module.css";

type Load = { status: "loading" } | { status: "ready" } | { status: "error"; failure: ApiFailure };
type Notice = { tone: "success" | "error"; text: string } | null;
type Conflict = "save" | "archive" | "restore" | null;
type Choice = { id: string; name: string; archived: boolean };

export const dictionaryConflictMessage = "Запись была изменена другим сотрудником. Ваши изменения не сохранены.";
export const notFoundMessage = "Запись не найдена.";

const names: Record<DictionaryKind, { one: string; new: string }> = {
  categories: { one: "Категория", new: "Новая категория" },
  manufacturers: { one: "Производитель", new: "Новый производитель" },
  brands: { one: "Бренд", new: "Новый бренд" },
  collections: { one: "Подборка", new: "Новая подборка" },
};

export const archiveExplanations: Record<DictionaryKind, string> = {
  categories:
    "Категория исчезнет из каталога и фильтров магазина и не будет предлагаться для товаров. " +
    "В архив можно перенести только категорию без товаров и без активных подкатегорий; вернуть её можно в любой момент.",
  manufacturers:
    "Производитель исчезнет из каталога магазина и не будет предлагаться для товаров и брендов. " +
    "В архив можно перенести только производителя без товаров и без активных брендов.",
  brands: "Бренд исчезнет из каталога магазина и не будет предлагаться для товаров. В архив можно перенести только бренд без товаров.",
  collections:
    "Подборка исчезнет из магазина и не будет предлагаться для новых товаров. " +
    "Товары останутся в ней и снова появятся в подборке после восстановления.",
};

/** "Нельзя перенести в архив: …" with the counts Go reported. */
export function referenceConflictText(references: References): string {
  const parts = [
    references.products ? `товаров — ${references.products}` : "",
    references.brands ? `активных брендов — ${references.brands}` : "",
    references.categories ? `активных подкатегорий — ${references.categories}` : "",
  ].filter(Boolean);
  return `Нельзя перенести в архив: запись используется (${parts.join(", ") || "есть ссылки"}). Сначала переназначьте или перенесите их в архив.`;
}

const fieldId = (field: DictionaryField) => `dictionary-${field}`;
const errorId = (field: DictionaryField) => `dictionary-${field}-error`;

/**
 * Creates (no entryId) or edits one dictionary entry. Like the product
 * editor, the form keeps the revision it was loaded at, sends it with every
 * write and replaces it only on an explicit reload.
 */
export function DictionaryEditor<K extends DictionaryKind>({ kind, entryId, created = false }: { kind: K; entryId?: string; created?: boolean }) {
  const router = useRouter();
  const { read, mutate } = useStaffApi();
  const [load, setLoad] = useState<Load>({ status: "loading" });
  const [entry, setEntry] = useState<DictionaryEntry[K] | null>(null);
  // Parent categories for a category, manufacturers for a brand.
  const [choices, setChoices] = useState<Array<Choice & { parentId?: string }>>([]);
  const [form, setForm] = useState<DictionaryFormValues>(emptyDictionaryForm);
  const [baseline, setBaseline] = useState<DictionaryFormValues>(emptyDictionaryForm);
  const [errors, setErrors] = useState<DictionaryFormErrors>({});
  const [formMessages, setFormMessages] = useState<string[]>([]);
  const [notice, setNotice] = useState<Notice>(created ? { tone: "success", text: "Запись создана." } : null);
  const [conflict, setConflict] = useState<Conflict>(null);
  const [busy, setBusy] = useState<"save" | "archive" | "restore" | "reload" | null>(null);
  const [confirmArchive, setConfirmArchive] = useState(false);
  const summaryRef = useRef<HTMLDivElement>(null);
  const conflictRef = useRef<HTMLDivElement>(null);
  const confirmRef = useRef<HTMLHeadingElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);

  const fields = dictionaryFields[kind];
  const dirty = !sameDictionaryForm(form, baseline);
  useUnsavedChanges(dirty);

  const adopt = useCallback(
    (next: DictionaryEntry[K]) => {
      const values = formFromEntry(kind, next);
      setEntry(next);
      setForm(values);
      setBaseline(values);
      setErrors({});
      setFormMessages([]);
    },
    [kind],
  );

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      const choiceKind = kind === "categories" ? "categories" : kind === "brands" ? "manufacturers" : null;
      const [entryResult, choiceResult] = await Promise.all([
        entryId ? read(() => getDictionaryEntry(kind, entryId, controller.signal)) : Promise.resolve(null),
        choiceKind ? read(() => listDictionary(choiceKind, "all", controller.signal)) : Promise.resolve(null),
      ]);
      if (controller.signal.aborted) return;
      if (entryResult && !entryResult.ok) return setLoad({ status: "error", failure: entryResult.error });
      if (choiceResult && !choiceResult.ok) return setLoad({ status: "error", failure: choiceResult.error });
      if (choiceResult) setChoices(choiceResult.data.map((row) => ({ id: row.id, name: row.name, archived: row.archived, parentId: (row as AdminCategory).parentId })));
      if (entryResult) adopt(entryResult.data);
      setLoad({ status: "ready" });
    })();
    return () => controller.abort();
  }, [adopt, entryId, kind, read]);

  const set = (field: DictionaryField, value: string) => setForm((current) => ({ ...current, [field]: value }));

  function focusNotice() {
    requestAnimationFrame(() => noticeRef.current?.focus());
  }

  function showFailure(failure: ApiFailure, action: Exclude<Conflict, null>) {
    switch (failure.kind) {
      case "stale_revision":
      case "conflict":
        setConflict(action);
        requestAnimationFrame(() => conflictRef.current?.focus());
        return;
      case "reference_conflict":
        setNotice({ tone: "error", text: referenceConflictText(failure.references) });
        return focusNotice();
      case "invalid_hierarchy":
        setErrors({ parentId: failureMessages.invalid_hierarchy });
        setFormMessages([]);
        requestAnimationFrame(() => summaryRef.current?.focus());
        return;
      case "validation": {
        if (action !== "save") {
          // Restore refuses an archived parent or manufacturer; there is no form error to point at.
          setNotice({ tone: "error", text: failure.fields.map((field) => field.message).join(" ") || failureMessages.validation });
          return focusNotice();
        }
        const mapped = dictionaryErrorsFromServer(kind, failure.fields);
        setErrors(mapped.errors);
        setFormMessages(mapped.other);
        requestAnimationFrame(() => summaryRef.current?.focus());
        return;
      }
      case "staff_changed":
        router.refresh();
        break;
      case "unauthorized":
        return;
    }
    setNotice({ tone: "error", text: failure.kind === "not_found" ? notFoundMessage : failureMessages[failure.kind] });
    focusNotice();
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || conflict) return;
    setNotice(null);
    const check = payloadFromDictionaryForm(kind, form);
    if (!check.ok) {
      setErrors(check.errors);
      setFormMessages([]);
      requestAnimationFrame(() => summaryRef.current?.focus());
      return;
    }
    setErrors({});
    setFormMessages([]);
    setBusy("save");
    const result = entry
      ? await mutate((auth) => updateDictionaryEntry(kind, entry.id, entry.revision, check.payload, auth))
      : await mutate((auth) => createDictionaryEntry(kind, check.payload, auth));
    setBusy(null);
    if (!result.ok) return showFailure(result.error, "save");
    if (!entry) {
      setBaseline(form);
      router.replace(`${entryHref(kind, result.data.id)}?created=1`);
      return;
    }
    adopt(result.data);
    setNotice({ tone: "success", text: `Изменения сохранены. Ревизия ${result.data.revision}.` });
  }

  async function reloadServerVersion() {
    if (!entry) return;
    setBusy("reload");
    const result = await read(() => getDictionaryEntry(kind, entry.id));
    setBusy(null);
    if (!result.ok) {
      if (result.error.kind !== "unauthorized") setNotice({ tone: "error", text: result.error.kind === "not_found" ? notFoundMessage : failureMessages[result.error.kind] });
      return;
    }
    adopt(result.data);
    setConflict(null);
    setNotice({ tone: "success", text: `Загружена актуальная версия. Ревизия ${result.data.revision}.` });
  }

  async function changeArchive(archive: boolean) {
    if (!entry || busy) return;
    setConfirmArchive(false);
    setNotice(null);
    setBusy(archive ? "archive" : "restore");
    const result = await mutate((auth) => setDictionaryEntryArchived(kind, entry.id, entry.revision, archive, auth));
    setBusy(null);
    if (!result.ok) return showFailure(result.error, archive ? "archive" : "restore");
    adopt(result.data);
    setNotice({ tone: "success", text: archive ? `Запись перенесена в архив. Ревизия ${result.data.revision}.` : `Запись восстановлена. Ревизия ${result.data.revision}.` });
  }

  const listHref = `/dictionaries/${kind}`;
  if (load.status === "loading") {
    return (
      <p aria-live="polite" className={styles.muted} role="status">
        Загрузка…
      </p>
    );
  }
  if (load.status === "error") {
    const missing = load.failure.kind === "not_found";
    return (
      <section className={styles.page}>
        <h1>{missing ? "Запись не найдена" : "Не удалось открыть запись"}</h1>
        <p className={styles.muted} role="alert">
          {missing ? notFoundMessage : failureMessages[load.failure.kind]}
        </p>
        <Link href={listHref}>К списку: {dictionaryTitles[kind].title.toLowerCase()}</Link>
      </section>
    );
  }

  const errorEntries = fields.filter((field) => errors[field]);
  const describedBy = (field: DictionaryField, hint?: boolean) => [hint ? `${fieldId(field)}-hint` : undefined, errors[field] ? errorId(field) : undefined].filter(Boolean).join(" ") || undefined;
  const control = (field: DictionaryField): { input: ReactNode; hint?: string } => {
    const common = {
      "aria-invalid": errors[field] ? true : undefined,
      id: fieldId(field),
      onChange: (event: { target: { value: string } }) => set(field, event.target.value),
      value: form[field],
    };
    switch (field) {
      case "description":
        return { input: <textarea {...common} aria-describedby={describedBy(field)} rows={4} /> };
      case "slug":
        return { input: <input {...common} aria-describedby={describedBy(field, true)} aria-required maxLength={120} type="text" />, hint: "Строчные латинские буквы, цифры и дефисы: dark-chocolate" };
      case "name":
        return { input: <input {...common} aria-describedby={describedBy(field)} aria-required maxLength={120} type="text" /> };
      case "sortOrder":
        return { input: <input {...common} aria-describedby={describedBy(field, true)} inputMode="numeric" type="text" />, hint: "Меньше — выше в списках магазина" };
      case "icon":
        return { input: <input {...common} aria-describedby={describedBy(field, true)} maxLength={40} type="text" />, hint: "Необязательно: например, chocolate или gift" };
      case "country":
        return { input: <input {...common} aria-describedby={describedBy(field)} maxLength={60} type="text" /> };
      case "accent":
        return {
          input: (
            <select {...common} aria-describedby={describedBy(field)}>
              {collectionAccents.map((accent) => (
                <option key={accent} value={accent}>
                  {accentLabels[accent]}
                </option>
              ))}
            </select>
          ),
        };
      case "startsAt":
      case "endsAt":
        return {
          input: <input {...common} aria-describedby={describedBy(field, true)} type="date" />,
          hint: field === "startsAt" ? "Пусто — без даты начала. Время московское" : "Пусто — без даты окончания. Включительно",
        };
      case "parentId": {
        const category = entry as AdminCategory | null;
        const locked = (category?.childCount ?? 0) > 0;
        const current = category?.parentId ?? "";
        const parents = choices.filter((choice) => !choice.parentId && choice.id !== category?.id && (!choice.archived || choice.id === current));
        return {
          input: (
            <select {...common} aria-describedby={describedBy(field, true)} disabled={locked}>
              <option value="">Нет — корневая категория</option>
              {parents.map((choice) => (
                <option key={choice.id} value={choice.id}>
                  {choice.archived ? `${choice.name} (в архиве)` : choice.name}
                </option>
              ))}
            </select>
          ),
          hint: locked ? "У категории есть подкатегории, поэтому она остаётся корневой" : "Каталог двухуровневый: родителем может быть только корневая категория",
        };
      }
      default: {
        const brand = entry as AdminBrand | null;
        const locked = (brand?.productCount ?? 0) > 0;
        const current = brand?.manufacturer.id ?? "";
        const makers = choices.filter((choice) => !choice.archived || choice.id === current);
        return {
          input: (
            <select {...common} aria-describedby={describedBy(field, true)} aria-required disabled={locked}>
              <option value="">Выберите производителя</option>
              {makers.map((choice) => (
                <option key={choice.id} value={choice.id}>
                  {choice.archived ? `${choice.name} (в архиве)` : choice.name}
                </option>
              ))}
            </select>
          ),
          hint: locked ? `Бренд используется в товарах (${brand?.productCount}), поэтому производителя сменить нельзя` : "Бренд принадлежит одному производителю",
        };
      }
    }
  };

  const title = entry ? entry.name : names[kind].new;
  return (
    <section aria-labelledby="dictionary-entry-title" className={styles.page}>
      <p className={styles.breadcrumb}>
        <Link href={listHref}>{dictionaryTitles[kind].title}</Link>
      </p>
      <DictionaryTabs current={kind} />
      <div className={styles.heading}>
        <h1 id="dictionary-entry-title">{title}</h1>
        {entry ? <p className={entry.archived ? styles.statusArchived : styles.statusActive}>{entry.archived ? "В архиве" : activeLabel[kind]}</p> : null}
      </div>
      {entry ? <Facts entry={entry} kind={kind} /> : null}
      {entry?.archived ? <p className={styles.archivedNote}>Запись в архиве: покупатели её не видят, для новых товаров она не предлагается. Её можно редактировать.</p> : null}

      <div aria-live="polite" role="status">
        {notice?.tone === "success" ? <p className={styles.success}>{notice.text}</p> : null}
      </div>
      {notice?.tone === "error" ? (
        <p className={styles.alert} ref={noticeRef} role="alert" tabIndex={-1}>
          {notice.text}
        </p>
      ) : null}

      {conflict ? (
        <div className={styles.conflict} ref={conflictRef} role="alert" tabIndex={-1}>
          <p>
            <strong>{dictionaryConflictMessage}</strong>
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
                router.push(listHref);
              }}
              type="button"
            >
              Отменить мои изменения и вернуться к списку
            </button>
          </div>
        </div>
      ) : null}

      {errorEntries.length || formMessages.length ? (
        <div aria-labelledby="dictionary-errors-title" className={styles.summary} ref={summaryRef} role="alert" tabIndex={-1}>
          <h2 id="dictionary-errors-title">Исправьте ошибки</h2>
          <ul>
            {errorEntries.map((field) => (
              <li key={field}>
                <a href={`#${fieldId(field)}`}>
                  {dictionaryFieldLabels[field]}: {errors[field]}
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
          <legend>{names[kind].one}</legend>
          {fields.map((field) => {
            const { input, hint } = control(field);
            return (
              <div className={field === "name" || field === "description" ? `${styles.field} ${styles.wide}` : styles.field} key={field}>
                <label htmlFor={fieldId(field)}>{dictionaryFieldLabels[field]}</label>
                {input}
                {hint ? (
                  <p className={styles.hint} id={`${fieldId(field)}-hint`}>
                    {hint}
                  </p>
                ) : null}
                {errors[field] ? (
                  <p className={styles.fieldError} id={errorId(field)}>
                    {errors[field]}
                  </p>
                ) : null}
              </div>
            );
          })}
        </fieldset>
        {entry ? <Media entry={entry} kind={kind} /> : null}
        <div className={styles.formActions}>
          <button className={styles.primaryButton} disabled={busy !== null || conflict !== null} type="submit">
            {busy === "save" ? "Сохраняем…" : entry ? "Сохранить изменения" : "Создать"}
          </button>
          {dirty ? <span className={styles.muted}>Есть несохранённые изменения</span> : null}
        </div>
      </form>

      {entry ? (
        <section aria-labelledby="dictionary-availability" className={styles.availability}>
          <h2 id="dictionary-availability">Архив</h2>
          {entry.archived ? (
            <>
              <p>Запись в архиве. Её можно вернуть в магазин и в списки для товаров.</p>
              <button className={styles.secondaryAction} disabled={busy !== null || conflict !== null || dirty} onClick={() => changeArchive(false)} type="button">
                {busy === "restore" ? "Восстанавливаем…" : "Восстановить"}
              </button>
            </>
          ) : confirmArchive ? (
            <div aria-labelledby="dictionary-archive-title" className={styles.confirm} role="group">
              <h3 id="dictionary-archive-title" ref={confirmRef} tabIndex={-1}>
                Перенести в архив?
              </h3>
              <p>{archiveExplanations[kind]}</p>
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
              <p>{archiveExplanations[kind]}</p>
              <button
                className={styles.secondaryAction}
                disabled={busy !== null || conflict !== null || dirty}
                onClick={() => {
                  setConfirmArchive(true);
                  requestAnimationFrame(() => confirmRef.current?.focus());
                }}
                type="button"
              >
                {busy === "archive" ? "Переносим…" : "Перенести в архив…"}
              </button>
            </>
          )}
          {dirty ? <p className={styles.muted}>Сначала сохраните или отмените изменения формы.</p> : null}
          <p className={styles.muted}>Записи справочников не удаляются.</p>
        </section>
      ) : null}
    </section>
  );
}

function Facts<K extends DictionaryKind>({ kind, entry }: { kind: K; entry: DictionaryEntry[K] }) {
  const usage: Array<[string, number]> = [["Товаров", entry.productCount]];
  if (kind === "categories") usage.push(["Подкатегорий", (entry as AdminCategory).childCount]);
  if (kind === "manufacturers") usage.push(["Брендов", (entry as AdminManufacturer).brandCount]);
  return (
    <dl className={styles.facts}>
      <div>
        <dt>ID</dt>
        <dd>{entry.id}</dd>
      </div>
      <div>
        <dt>Ревизия</dt>
        <dd data-testid="dictionary-revision">{entry.revision}</dd>
      </div>
      {usage.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function Media<K extends DictionaryKind>({ kind, entry }: { kind: K; entry: DictionaryEntry[K] }) {
  if (kind === "categories") return null;
  const path = kind === "collections" ? (entry as AdminCollection).image : (entry as AdminManufacturer | AdminBrand).logo;
  return (
    <section aria-labelledby="dictionary-media" className={styles.media}>
      <h2 id="dictionary-media">{kind === "collections" ? "Изображение" : "Логотип"}</h2>
      {path ? <code>{path}</code> : <p>Не задано.</p>}
      <p className={styles.muted}>Только просмотр: управление изображениями будет добавлено отдельно.</p>
    </section>
  );
}
