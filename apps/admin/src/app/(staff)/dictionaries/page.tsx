import { redirect } from "next/navigation";

// The dictionaries section opens on categories.
export default function DictionariesPage() {
  redirect("/dictionaries/categories");
}
