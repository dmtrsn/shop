"use client";

import { RouteError } from "@/components/storefront/route-states";

export default function RepeatError() {
  return <RouteError title="Не удалось открыть повтор заказа" link={{ href: "/account/orders", label: "Вернуться к заказам", prefetch: false }}>
    Если добавление уже отправлено, его результат может быть неизвестен. Не отправляйте тот же повтор заново, пока не уточните результат.
  </RouteError>;
}
