"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function OrdersError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть историю заказов" reset={reset} link={{ href: "/catalog", label: "Перейти в каталог" }}>
    Попробуйте загрузить страницу ещё раз.
  </RouteError>;
}
