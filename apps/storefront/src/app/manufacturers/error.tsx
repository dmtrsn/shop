"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function ManufacturersError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить производителей" reset={reset} link={{ href: "/catalog", label: "Открыть каталог" }} digest={error.digest}>
    Сервис временно недоступен или вернул данные в неожиданном формате.
  </RouteError>;
}
