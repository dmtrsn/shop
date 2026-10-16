import { RouteNotFound } from "@/components/storefront/route-states";

export default function CategoryNotFound() {
  return <RouteNotFound links={[{ href: "/categories", label: "К списку категорий" }]} title="Категория не найдена">
    Возможно, ссылка устарела или категория больше не опубликована.
  </RouteNotFound>;
}
