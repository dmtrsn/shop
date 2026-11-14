"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function ErrorBoundary({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть быстрый заказ" reset={reset} digest={error.digest} link={{ href: "/catalog", label: "В каталог" }}>
    Не удалось загрузить производителей. Попробуйте ещё раз.
  </RouteError>;
}
