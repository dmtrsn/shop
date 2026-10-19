"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function RouteErrorBoundary({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть страницу восстановления пароля" reset={reset}>
    Попробуйте ещё раз.
  </RouteError>;
}
