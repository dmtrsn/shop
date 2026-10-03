// Formats an authoritative integer minor-unit amount. It never does arithmetic
// on prices or totals; Go computes every financial value.
export function formatMoney(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat("ru-RU", { currency, style: "currency" }).format(amountMinor / 100);
}
