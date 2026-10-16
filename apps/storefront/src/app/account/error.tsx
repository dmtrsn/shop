"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function AccountError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть аккаунт" reset={reset}>
    Попробуйте загрузить страницу ещё раз.
  </RouteError>;
}
