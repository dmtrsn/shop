"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function HomeError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть главную страницу" reset={reset} link={{ href: "/catalog", label: "Открыть каталог" }} digest={error.digest}>
    Произошла непредвиденная ошибка. Попробуйте ещё раз или перейдите в каталог.
  </RouteError>;
}
