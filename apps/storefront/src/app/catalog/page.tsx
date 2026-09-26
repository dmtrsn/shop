import { redirect } from "next/navigation";

import { CatalogView } from "@/components/storefront/catalog-view";
import { getCatalogData } from "@/lib/api/catalog";
import { canonicalizeCatalogIdentity } from "@/lib/catalog-canonicalization";
import {
  catalogUrl,
  parseCatalogState,
  type CatalogSearchParams,
} from "@/lib/catalog-state";

type CatalogPageProps = {
  searchParams: Promise<CatalogSearchParams>;
};

export default async function CatalogPage({ searchParams }: CatalogPageProps) {
  const state = parseCatalogState(await searchParams);
  const data = await getCatalogData(state);
  const corrected = canonicalizeCatalogIdentity(state, data);

  if (
    corrected.category !== state.category ||
    corrected.manufacturer !== state.manufacturer ||
    corrected.brand !== state.brand ||
    corrected.series !== state.series
  ) {
    redirect(catalogUrl({ ...corrected, page: 1 }, {}, { resetPage: false }));
  }

  const lastUsefulPage = data.page.totalPages || 1;
  if (state.page > lastUsefulPage) {
    redirect(catalogUrl(state, { page: lastUsefulPage }, { resetPage: false }));
  }

  return <CatalogView data={data} state={state} />;
}
