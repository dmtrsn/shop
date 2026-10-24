import type { Metadata } from "next";

import { OrderDetail } from "@/components/order-detail";

export const metadata: Metadata = { title: "Заказ" };

type Props = { params: Promise<{ id: string }> };

export default async function OrderPage({ params }: Props) {
  const { id } = await params;
  return <OrderDetail key={id} orderId={id} />;
}
