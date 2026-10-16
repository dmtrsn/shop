"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function OrderError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть заказ" reset={reset} link={{ href: "/catalog", label: "Перейти в каталог" }}>
    Попробуйте загрузить страницу ещё раз.
  </RouteError>;
}
