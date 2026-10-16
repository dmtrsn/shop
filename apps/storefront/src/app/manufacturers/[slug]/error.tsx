"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function ManufacturerError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить производителя" reset={reset} link={{ href: "/manufacturers", label: "К списку производителей" }} digest={error.digest}>
    Сервис временно недоступен. Попробуйте запрос ещё раз.
  </RouteError>;
}
