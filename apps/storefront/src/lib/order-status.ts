export type OrderStatusTone = "success" | "neutral";

// Visual tone only for status codes Go actually produces ("processing" and
// "delivered"). The label is always Go's statusLabel; an unknown or future
// status is shown truthfully with the neutral tone, never guessed.
export function orderStatusTone(status: string): OrderStatusTone {
  return status === "delivered" ? "success" : "neutral";
}
