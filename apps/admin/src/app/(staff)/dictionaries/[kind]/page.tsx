import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { DictionaryList } from "@/components/dictionary-list";
import { isDictionaryKind } from "@/lib/dictionaries-api";
import { dictionaryTitles, parseDictionaryStatus } from "@/lib/dictionary-query";

type Props = {
  params: Promise<{ kind: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { kind } = await params;
  return { title: isDictionaryKind(kind) ? dictionaryTitles[kind].title : "Справочники" };
}

// Like the product list, the entries load in the browser; nothing is cached server-side.
export default async function DictionaryPage({ params, searchParams }: Props) {
  const { kind } = await params;
  if (!isDictionaryKind(kind)) notFound();
  const status = parseDictionaryStatus((await searchParams).status);
  return <DictionaryList key={kind} kind={kind} status={status} />;
}
