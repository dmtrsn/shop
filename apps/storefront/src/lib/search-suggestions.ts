import { catalogUrl, parseCatalogState } from "./catalog-state";
import { categoryUrl } from "./category-tree";

// Mirrors Service.Suggestions/validateSearchQuery in internal/application/service.go (Unicode code points).
export const SUGGESTION_MIN_QUERY_LENGTH = 2;
export const SEARCH_QUERY_MAX_LENGTH = 200;

export const SUGGESTION_TYPES = ["product", "category", "manufacturer", "brand"] as const;

export type SuggestionType = (typeof SUGGESTION_TYPES)[number];

export type SearchSuggestion = {
  type: SuggestionType;
  id: string;
  slug: string;
  title: string;
  subtitle?: string;
  image?: string;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requiredString(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value !== "string" || !value) throw new Error(`${key} must be a non-empty string`);
  return value;
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${key} must be a string`);
  return value || undefined;
}

function isSuggestionType(value: unknown): value is SuggestionType {
  return SUGGESTION_TYPES.includes(value as SuggestionType);
}

/** Go-rune-compatible length of a well-formed string. */
export function codePointLength(value: string): number {
  return Array.from(value).length;
}

/**
 * Parses the `{items}` suggestion envelope. Types added to the backend later are skipped
 * until the storefront knows their canonical destination; malformed known items fail.
 */
export function parseSuggestions(value: unknown): SearchSuggestion[] {
  if (!isRecord(value) || !Array.isArray(value.items)) {
    throw new Error("suggestions must be an object with an items array");
  }
  const result: SearchSuggestion[] = [];
  for (const item of value.items) {
    if (!isRecord(item)) throw new Error("suggestion must be an object");
    if (!isSuggestionType(item.type)) continue;
    result.push({
      type: item.type,
      id: requiredString(item, "id"),
      slug: requiredString(item, "slug"),
      title: requiredString(item, "title"),
      subtitle: optionalString(item, "subtitle"),
      image: optionalString(item, "image"),
    });
  }
  return result;
}

export function suggestionHref(suggestion: SearchSuggestion): string {
  switch (suggestion.type) {
    case "product":
      return `/products/${encodeURIComponent(suggestion.slug)}`;
    case "category":
      return categoryUrl(suggestion.slug);
    case "manufacturer":
      return `/manufacturers/${encodeURIComponent(suggestion.slug)}`;
    case "brand":
      // No brand route exists yet, and the payload carries no manufacturer slug.
      return catalogUrl(parseCatalogState({}), { brand: suggestion.slug });
  }
}

export function searchResultsUrl(query: string): string {
  const trimmed = query.trim();
  return catalogUrl(parseCatalogState({}), { query: trimmed || undefined });
}

export async function fetchSuggestions(
  query: string,
  options: { fetcher?: typeof fetch; signal?: AbortSignal } = {},
): Promise<SearchSuggestion[]> {
  const fetcher = options.fetcher ?? fetch;
  const response = await fetcher(
    `/api/search/suggest?${new URLSearchParams({ q: query }).toString()}`,
    { headers: { Accept: "application/json" }, signal: options.signal },
  );
  if (!response.ok) throw new Error(`suggestions request failed with ${response.status}`);
  return parseSuggestions(await response.json());
}
