"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function LoginError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть страницу входа" reset={reset}>
    Попробуйте ещё раз.
  </RouteError>;
}
