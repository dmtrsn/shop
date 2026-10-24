"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";

import { failureMessages } from "@/lib/admin-api";
import {
  dictionaryKinds,
  listDictionary,
  type AdminBrand,
  type AdminCategory,
  type AdminCollection,
  type AdminManufacturer,
  type DictionaryEntry,
  type DictionaryKind,
  type DictionaryStatus,
} from "@/lib/dictionaries-api";
import { moscowDay } from "@/lib/dictionary-form";
import { activeLabel, dictionaryListHref, dictionaryStatuses, dictionaryStatusLabels, dictionaryTitles, entryHref } from "@/lib/dictionary-query";
import { useStaffApi } from "./use-staff-api";
import styles from "./admin-pages.module.css";

/** The four dictionaries as sibling sections. */
export function DictionaryTabs({ current }: { current: DictionaryKind }) {
  return (
    <nav aria-label="Справочники" className={styles.tabs}>
      <ul>
        {dictionaryKinds.map((kind) => (
          <li key={kind}>
            <Link aria-current={kind === current ? "page" : undefined} href={`/dictionaries/${kind}`}>
              {dictionaryTitles[kind].title}
            </Link>
          </li>
        ))}
      </ul>
    </nav>
  );
}

type State<K extends DictionaryKind> = { status: "loading" } | { status: "ready"; items: Array<DictionaryEntry[K]> } | { status: "error"; message: string };

/**
 * One dictionary's entries. The URL holds the status filter; Go returns the
 * entries in storefront order, and categories are shown as their tree.
 */
export function DictionaryList<K extends DictionaryKind>({ kind, status }: { kind: K; status: DictionaryStatus }) {
  const router = useRouter();
  const { read } = useStaffApi();
  const [state, setState] = useState<State<K>>({ status: "loading" });
  const [reload, setReload] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setState({ status: "loading" });
    void read(() => listDictionary(kind, status, controller.signal)).then((result) => {
      if (controller.signal.aborted) return;
      setState(result.ok ? { status: "ready", items: result.data } : { status: "error", message: failureMessages[result.error.kind] });
    });
    return () => controller.abort();
  }, [kind, read, reload, status]);

  const titles = dictionaryTitles[kind];
  return (
    <section aria-labelledby="dictionary-title" className={styles.page}>
      <p className={styles.breadcrumb}>Справочники каталога</p>
      <DictionaryTabs current={kind} />
      <div className={styles.heading}>
        <h1 id="dictionary-title">{titles.title}</h1>
        <Link className={styles.primaryAction} href={`/dictionaries/${kind}/new`}>
          {titles.create}
        </Link>
      </div>
      <div className={`${styles.filters} ${styles.compactFilters}`}>
        <div className={styles.filterField}>
          <label htmlFor="dictionary-status">Статус</label>
          <select id="dictionary-status" onChange={(event) => router.push(dictionaryListHref(kind, event.target.value as DictionaryStatus))} value={status}>
            {dictionaryStatuses.map((value) => (
              <option key={value} value={value}>
                {dictionaryStatusLabels[value]}
              </option>
            ))}
          </select>
        </div>
      </div>
      {state.status === "loading" ? (
        <p aria-live="polite" className={styles.muted} role="status">
          Загрузка…
        </p>
      ) : state.status === "error" ? (
        <div className={styles.alert} role="alert">
          <p>Не удалось загрузить справочник. {state.message}</p>
          <button className={styles.secondaryAction} onClick={() => setReload((value) => value + 1)} type="button">
            Повторить
          </button>
        </div>
      ) : state.items.length === 0 ? (
        <div className={styles.empty} role="status">
          <p>{status === "archived" ? "В архиве ничего нет." : titles.empty}</p>
        </div>
      ) : (
        <>
          <p aria-live="polite" className={styles.muted} role="status">
            Записей: {state.items.length}
          </p>
          <div className={styles.tableWrap}>
            <Table items={state.items} kind={kind} />
          </div>
        </>
      )}
    </section>
  );
}

function Status({ kind, archived }: { kind: DictionaryKind; archived: boolean }) {
  return <span className={archived ? styles.statusArchived : styles.statusActive}>{archived ? "В архиве" : activeLabel[kind]}</span>;
}

function NameCell({ kind, entry, depth = 0 }: { kind: DictionaryKind; entry: { id: string; name: string; slug: string }; depth?: number }) {
  return (
    <td>
      <Link className={styles.productLink} href={entryHref(kind, entry.id)}>
        {depth ? <span aria-hidden="true">— </span> : null}
        {entry.name}
      </Link>
      <span className={styles.meta}>
        {entry.slug} · {entry.id}
      </span>
    </td>
  );
}

function Table<K extends DictionaryKind>({ kind, items }: { kind: K; items: Array<DictionaryEntry[K]> }) {
  const title = dictionaryTitles[kind].title;
  const head = (cells: ReactNode) => (
    <thead>
      <tr>{cells}</tr>
    </thead>
  );
  const number = (label: string, secondary = false) => (
    <th className={secondary ? `${styles.number} ${styles.secondary}` : styles.number} scope="col">
      {label}
    </th>
  );
  switch (kind) {
    case "categories": {
      const categories = items as AdminCategory[];
      const names = new Map(categories.map((category) => [category.id, category.name]));
      return (
        <table className={styles.table}>
          <caption className={styles.visuallyHidden}>{title}</caption>
          {head(
            <>
              <th scope="col">Категория</th>
              <th className={styles.secondary} scope="col">
                Родитель
              </th>
              {number("Товаров")}
              {number("Подкатегорий", true)}
              <th scope="col">Статус</th>
            </>,
          )}
          <tbody>
            {categoryRows(categories).map(({ category, depth }) => (
              <tr key={category.id}>
                <NameCell depth={depth} entry={category} kind={kind} />
                <td className={styles.secondary}>{category.parentId ? (names.get(category.parentId) ?? category.parentId) : "—"}</td>
                <td className={styles.number}>{category.productCount}</td>
                <td className={`${styles.number} ${styles.secondary}`}>{category.childCount}</td>
                <td>
                  <Status archived={category.archived} kind={kind} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    }
    case "manufacturers":
      return (
        <table className={styles.table}>
          <caption className={styles.visuallyHidden}>{title}</caption>
          {head(
            <>
              <th scope="col">Производитель</th>
              <th className={styles.secondary} scope="col">
                Страна
              </th>
              {number("Брендов", true)}
              {number("Товаров")}
              <th scope="col">Статус</th>
            </>,
          )}
          <tbody>
            {(items as AdminManufacturer[]).map((manufacturer) => (
              <tr key={manufacturer.id}>
                <NameCell entry={manufacturer} kind={kind} />
                <td className={styles.secondary}>{manufacturer.country || "—"}</td>
                <td className={`${styles.number} ${styles.secondary}`}>{manufacturer.brandCount}</td>
                <td className={styles.number}>{manufacturer.productCount}</td>
                <td>
                  <Status archived={manufacturer.archived} kind={kind} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    case "brands":
      return (
        <table className={styles.table}>
          <caption className={styles.visuallyHidden}>{title}</caption>
          {head(
            <>
              <th scope="col">Бренд</th>
              <th className={styles.secondary} scope="col">
                Производитель
              </th>
              {number("Товаров")}
              <th scope="col">Статус</th>
            </>,
          )}
          <tbody>
            {(items as AdminBrand[]).map((brand) => (
              <tr key={brand.id}>
                <NameCell entry={brand} kind={kind} />
                <td className={styles.secondary}>{brand.manufacturer.name}</td>
                <td className={styles.number}>{brand.productCount}</td>
                <td>
                  <Status archived={brand.archived} kind={kind} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
    default:
      return (
        <table className={styles.table}>
          <caption className={styles.visuallyHidden}>{title}</caption>
          {head(
            <>
              <th scope="col">Подборка</th>
              <th className={styles.secondary} scope="col">
                Период показа
              </th>
              {number("Товаров")}
              <th scope="col">Статус</th>
            </>,
          )}
          <tbody>
            {(items as AdminCollection[]).map((collection) => (
              <tr key={collection.id}>
                <NameCell entry={collection} kind={kind} />
                <td className={styles.secondary}>{collectionPeriod(collection)}</td>
                <td className={styles.number}>{collection.productCount}</td>
                <td>
                  <Status archived={collection.archived} kind={kind} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      );
  }
}

const day = (value: string) => value.split("-").reverse().join(".");

export function collectionPeriod(collection: Pick<AdminCollection, "startsAt" | "endsAt">): string {
  const from = moscowDay(collection.startsAt);
  const to = moscowDay(collection.endsAt);
  if (!from && !to) return "Всегда";
  if (!to) return `с ${day(from)}`;
  if (!from) return `по ${day(to)}`;
  return `${day(from)} – ${day(to)}`;
}

/** Top-level categories, each followed by its subcategories; orphans (a parent outside the list) go last. */
export function categoryRows(categories: AdminCategory[]): Array<{ category: AdminCategory; depth: number }> {
  const listed = new Set(categories.map((category) => category.id));
  const rows: Array<{ category: AdminCategory; depth: number }> = [];
  for (const top of categories.filter((category) => !category.parentId)) {
    rows.push({ category: top, depth: 0 });
    for (const child of categories.filter((category) => category.parentId === top.id)) rows.push({ category: child, depth: 1 });
  }
  for (const orphan of categories.filter((category) => category.parentId && !listed.has(category.parentId))) rows.push({ category: orphan, depth: 1 });
  return rows;
}
