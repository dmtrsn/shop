"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function CollectionsError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить подборки" reset={reset} link={{ href: "/catalog", label: "Открыть каталог" }} digest={error.digest}>
    Сервис временно недоступен или вернул некорректный ответ. Попробуйте ещё раз.
  </RouteError>;
}
