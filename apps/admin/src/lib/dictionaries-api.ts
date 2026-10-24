// Typed client for the staff catalog dictionaries (api/admin-openapi.yaml,
// ADR 018): categories, manufacturers, brands and collections. Each keeps its
// own shape; only the revision and archive plumbing is shared.

import { adminRequest, isRecord, type ApiResult, type MutationAuth } from "./admin-api";
import type { Reference } from "./products-api";

export const dictionaryKinds = ["categories", "manufacturers", "brands", "collections"] as const;
export type DictionaryKind = (typeof dictionaryKinds)[number];
export type DictionaryStatus = "active" | "archived" | "all";

type DictionaryState = {
  id: string;
  revision: number;
  archived: boolean;
  archivedAt?: string;
  // Products, archived or not, that use the entry; for a collection, its members.
  productCount: number;
  slug: string;
  name: string;
  description: string;
  sortOrder: number;
};

export type AdminCategory = DictionaryState & { parentId?: string; icon: string; childCount: number };
export type AdminManufacturer = DictionaryState & { country: string; logo: string; brandCount: number };
export type AdminBrand = DictionaryState & { manufacturer: Reference; logo: string };
export type AdminCollection = DictionaryState & { accent: CollectionAccent; image: string; startsAt?: string; endsAt?: string };

export const collectionAccents = ["lime", "cocoa", "coral", "sky"] as const;
export type CollectionAccent = (typeof collectionAccents)[number];

export type DictionaryEntry = {
  categories: AdminCategory;
  manufacturers: AdminManufacturer;
  brands: AdminBrand;
  collections: AdminCollection;
};

/** The write documents: every member is required. */
export type DictionaryPayload = {
  categories: { slug: string; name: string; description: string; parentId: string; icon: string; sortOrder: number };
  manufacturers: { slug: string; name: string; country: string; description: string; sortOrder: number };
  brands: { slug: string; name: string; manufacturerId: string; description: string; sortOrder: number };
  collections: { slug: string; name: string; description: string; accent: CollectionAccent; startsAt: string | null; endsAt: string | null; sortOrder: number };
};

export function isDictionaryKind(value: string): value is DictionaryKind {
  return (dictionaryKinds as readonly string[]).includes(value);
}

const text = (body: Record<string, unknown>, keys: string[]) => keys.every((key) => typeof body[key] === "string");
const counts = (body: Record<string, unknown>, keys: string[]) => keys.every((key) => Number.isSafeInteger(body[key]));
const optionalText = (body: Record<string, unknown>, keys: string[]) => keys.every((key) => body[key] === undefined || typeof body[key] === "string");

function parseState(body: unknown): Record<string, unknown> | null {
  if (!isRecord(body) || !text(body, ["id", "slug", "name", "description"]) || !counts(body, ["revision", "productCount", "sortOrder"])) return null;
  if (typeof body.archived !== "boolean" || !optionalText(body, ["archivedAt"])) return null;
  return body;
}

// Checks the members the UI relies on; the contract test pins the rest.
const parsers: { [K in DictionaryKind]: (body: unknown) => DictionaryEntry[K] | null } = {
  categories: (body) => {
    const row = parseState(body);
    return row && text(row, ["icon"]) && counts(row, ["childCount"]) && optionalText(row, ["parentId"]) ? (row as unknown as AdminCategory) : null;
  },
  manufacturers: (body) => {
    const row = parseState(body);
    return row && text(row, ["country", "logo"]) && counts(row, ["brandCount"]) ? (row as unknown as AdminManufacturer) : null;
  },
  brands: (body) => {
    const row = parseState(body);
    const manufacturer = row?.manufacturer;
    return row && text(row, ["logo"]) && isRecord(manufacturer) && text(manufacturer, ["id", "name"]) ? (row as unknown as AdminBrand) : null;
  },
  collections: (body) => {
    const row = parseState(body);
    return row && text(row, ["accent", "image"]) && optionalText(row, ["startsAt", "endsAt"]) ? (row as unknown as AdminCollection) : null;
  },
};

function parseList<K extends DictionaryKind>(kind: K) {
  return (body: unknown): Array<DictionaryEntry[K]> | null => {
    if (!isRecord(body) || !Array.isArray(body.items)) return null;
    const items = body.items.map(parsers[kind]);
    return items.some((item) => item === null) ? null : (items as Array<DictionaryEntry[K]>);
  };
}

const base = (kind: DictionaryKind) => `/api/admin/v1/${kind}`;
const entryPath = (kind: DictionaryKind, id: string) => `${base(kind)}/${encodeURIComponent(id)}`;

export function listDictionary<K extends DictionaryKind>(kind: K, status: DictionaryStatus, signal?: AbortSignal): Promise<ApiResult<Array<DictionaryEntry[K]>>> {
  return adminRequest(`${base(kind)}?status=${status}`, parseList(kind), { signal });
}

export function getDictionaryEntry<K extends DictionaryKind>(kind: K, id: string, signal?: AbortSignal): Promise<ApiResult<DictionaryEntry[K]>> {
  return adminRequest(entryPath(kind, id), parsers[kind], { signal });
}

export function createDictionaryEntry<K extends DictionaryKind>(kind: K, payload: DictionaryPayload[K], auth: MutationAuth): Promise<ApiResult<DictionaryEntry[K]>> {
  return adminRequest(base(kind), parsers[kind], { method: "POST", body: payload, auth });
}

export function updateDictionaryEntry<K extends DictionaryKind>(
  kind: K,
  id: string,
  expectedRevision: number,
  payload: DictionaryPayload[K],
  auth: MutationAuth,
): Promise<ApiResult<DictionaryEntry[K]>> {
  return adminRequest(entryPath(kind, id), parsers[kind], { method: "PUT", body: { ...payload, expectedRevision }, auth });
}

export function setDictionaryEntryArchived<K extends DictionaryKind>(
  kind: K,
  id: string,
  expectedRevision: number,
  archived: boolean,
  auth: MutationAuth,
): Promise<ApiResult<DictionaryEntry[K]>> {
  return adminRequest(`${entryPath(kind, id)}/${archived ? "archive" : "restore"}`, parsers[kind], { method: "POST", body: { expectedRevision }, auth });
}
