"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function CatalogError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить товары" reset={reset} link={{ href: "/", label: "На главную" }} digest={error.digest}>
    Сервис не ответил или вернул данные в неожиданном формате. Попробуйте ещё раз.
  </RouteError>;
}
