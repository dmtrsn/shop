"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function RegisterError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть регистрацию" reset={reset}>
    Попробуйте ещё раз.
  </RouteError>;
}
