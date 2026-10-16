import { RouteNotFound } from "@/components/storefront/route-states";

// Next keeps the 404 status and noindex; this only supplies the Russian copy.
export default function NotFound() {
  return <RouteNotFound links={[{ href: "/", label: "На главную" }, { href: "/catalog", label: "Перейти в каталог" }]} title="Страница не найдена">
    Возможно, ссылка устарела или страница была перемещена.
  </RouteNotFound>;
}
