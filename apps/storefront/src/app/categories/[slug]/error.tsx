"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function CategoryError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить категорию" reset={reset} link={{ href: "/categories", label: "К списку категорий" }} digest={error.digest}>
    Сервис временно недоступен. Попробуйте запрос ещё раз.
  </RouteError>;
}
