import type { Metadata } from "next";

import { OrderList } from "@/components/order-list";
import { parseOrderQuery } from "@/lib/order-query";

export const metadata: Metadata = { title: "Заказы" };

type Props = { searchParams: Promise<Record<string, string | string[] | undefined>> };

// Behind the (staff) session gate; orders load in the browser, never cached.
export default async function OrdersPage({ searchParams }: Props) {
  return <OrderList query={parseOrderQuery(await searchParams)} />;
}
