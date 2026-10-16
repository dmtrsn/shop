import { RouteNotFound } from "@/components/storefront/route-states";

export default function ManufacturerNotFound() {
  return <RouteNotFound links={[{ href: "/manufacturers", label: "К списку производителей" }]} title="Производитель не найден">
    Возможно, ссылка устарела или производитель больше не опубликован.
  </RouteNotFound>;
}
