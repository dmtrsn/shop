import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { DictionaryEditor } from "@/components/dictionary-editor";
import { isDictionaryKind } from "@/lib/dictionaries-api";

export const metadata: Metadata = { title: "Новая запись справочника" };

type Props = { params: Promise<{ kind: string }> };

export default async function NewDictionaryEntryPage({ params }: Props) {
  const { kind } = await params;
  if (!isDictionaryKind(kind)) notFound();
  return <DictionaryEditor key={kind} kind={kind} />;
}
