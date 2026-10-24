import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { DictionaryEditor } from "@/components/dictionary-editor";
import { isDictionaryKind } from "@/lib/dictionaries-api";

export const metadata: Metadata = { title: "Запись справочника" };

type Props = {
  params: Promise<{ kind: string; id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

// Keyed by dictionary and ID, so moving to another entry starts a fresh editor.
export default async function DictionaryEntryPage({ params, searchParams }: Props) {
  const { kind, id } = await params;
  if (!isDictionaryKind(kind)) notFound();
  const created = (await searchParams).created === "1";
  return <DictionaryEditor created={created} entryId={id} key={`${kind}/${id}`} kind={kind} />;
}
