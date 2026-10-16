"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function CollectionError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось загрузить подборку" reset={reset} link={{ href: "/collections", label: "К списку подборок" }} digest={error.digest}>
    Сервис временно недоступен или вернул некорректный ответ. Попробуйте ещё раз.
  </RouteError>;
}
