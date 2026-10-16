"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function ProductError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить товар" reset={reset} link={{ href: "/", label: "На главную" }} digest={error.digest}>
    Сервис временно недоступен. Попробуйте запрос ещё раз.
  </RouteError>;
}
