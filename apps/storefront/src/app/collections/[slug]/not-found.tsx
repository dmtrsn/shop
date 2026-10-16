import { RouteNotFound } from "@/components/storefront/route-states";

export default function CollectionNotFound() {
  return <RouteNotFound links={[{ href: "/collections", label: "К списку подборок" }]} title="Подборка не найдена">
    Возможно, ссылка устарела или подборка сейчас недоступна.
  </RouteNotFound>;
}
