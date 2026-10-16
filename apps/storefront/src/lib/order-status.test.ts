import { describe, expect, it } from "vitest";

import { orderStatusTone } from "./order-status";

describe("orderStatusTone", () => {
  it.each([
    ["delivered", "success"],
    ["processing", "neutral"],
    ["", "neutral"],
    ["cancelled", "neutral"],
    ["Delivered", "neutral"],
  ])("%j → %s", (status, tone) => {
    expect(orderStatusTone(status)).toBe(tone);
  });
});
