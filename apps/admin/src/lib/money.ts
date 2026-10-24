// Rouble amounts as the operator types and reads them, converted to and from
// the integer kopecks the staff API stores (ADR 013). Conversion parses
// strings and uses integer arithmetic only: no binary floating point touches
// an amount, so nothing can be rounded silently.

/** The B1 upper bound, mirrored for early feedback; Go still enforces it. */
export const MAX_PRICE_MINOR = 1_000_000_000;

export type MoneyParse = { ok: true; minor: number } | { ok: false; reason: "empty" | "format" | "range" };

// Whole roubles, optionally with one decimal separator ("." or ",") and one or
// two kopeck digits. No signs, exponents, spaces or thousands separators.
const AMOUNT = /^(\d+)(?:[.,](\d{1,2}))?$/;

export function parseRubles(input: string): MoneyParse {
  const value = input.trim();
  if (!value) return { ok: false, reason: "empty" };
  const match = AMOUNT.exec(value);
  if (!match) return { ok: false, reason: "format" };
  const roubles = match[1].replace(/^0+(?=\d)/, "");
  // Longer than the bound's digits: out of range before any arithmetic.
  if (roubles.length > String(MAX_PRICE_MINOR / 100).length) return { ok: false, reason: "range" };
  const minor = Number(roubles) * 100 + Number((match[2] ?? "").padEnd(2, "0"));
  if (minor > MAX_PRICE_MINOR) return { ok: false, reason: "range" };
  return { ok: true, minor };
}

/** The editable form of an amount: "199,90", "199,00". */
export function roublesInput(minor: number): string {
  return `${Math.trunc(minor / 100)},${String(minor % 100).padStart(2, "0")}`;
}

/** Display form with grouped thousands: "1 299,90 ₽". */
export function formatRoubles(minor: number): string {
  const whole = String(Math.trunc(minor / 100)).replace(/\B(?=(\d{3})+(?!\d))/g, "\u00a0");
  return `${whole},${String(minor % 100).padStart(2, "0")}\u00a0₽`;
}
