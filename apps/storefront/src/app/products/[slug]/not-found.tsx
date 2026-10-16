import { RouteNotFound } from "@/components/storefront/route-states";

export default function ProductNotFound() {
  return <RouteNotFound links={[{ href: "/", label: "На главную" }, { href: "/catalog", label: "Перейти в каталог" }]} title="Товар не найден">
    Возможно, ссылка устарела или товар больше недоступен.
  </RouteNotFound>;
}
