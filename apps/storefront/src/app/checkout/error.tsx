"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function CheckoutError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return <RouteError title="Не удалось открыть оформление заказа" reset={reset} link={{ href: "/cart", label: "Вернуться в корзину" }}>
    Попробуйте загрузить страницу ещё раз.
  </RouteError>;
}
