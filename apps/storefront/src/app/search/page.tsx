import { redirect } from "next/navigation";

import {
  catalogUrl,
  parseCatalogState,
  type CatalogSearchParams,
} from "@/lib/catalog-state";

type SearchPageProps = {
  searchParams: Promise<CatalogSearchParams>;
};

// /catalog?q= is the canonical full-results page until a dedicated search layout exists.
export default async function SearchPage({ searchParams }: SearchPageProps) {
  redirect(catalogUrl(parseCatalogState(await searchParams), {}, { resetPage: false }));
}
