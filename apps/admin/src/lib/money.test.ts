import { describe, expect, it } from "vitest";

import { formatRoubles, MAX_PRICE_MINOR, parseRubles, roublesInput } from "./money";

describe("rouble amounts", () => {
  it.each([
    ["0,01", 1],
    ["0.01", 1],
    ["199", 19900],
    ["199.90", 19990],
    ["199,9", 19990],
    ["0199,90", 19990],
    ["  12,05 ", 1205],
    ["0", 0],
    ["10000000", MAX_PRICE_MINOR],
    ["10000000,00", MAX_PRICE_MINOR],
    // Classic binary-float traps stay exact.
    ["1.15", 115],
    ["4.35", 435],
    ["1005.17", 100517],
  ])("parses %s exactly", (input, minor) => {
    expect(parseRubles(input)).toEqual({ ok: true, minor });
  });

  it.each([
    ["", "empty"],
    ["   ", "empty"],
    ["199.999", "format"],
    ["1,2,3", "format"],
    ["-5", "format"],
    ["+5", "format"],
    ["1e3", "format"],
    ["1 000", "format"],
    ["12.", "format"],
    [",5", "format"],
    ["abc", "format"],
    ["NaN", "format"],
    ["10000000,01", "range"],
    ["99999999", "range"],
    ["123456789012345678901234567890", "range"],
  ])("rejects %j as %s", (input, reason) => {
    expect(parseRubles(input)).toEqual({ ok: false, reason });
  });

  it("round-trips kopecks to input and display text", () => {
    for (const minor of [0, 1, 99, 100, 19990, 48900, MAX_PRICE_MINOR]) {
      expect(parseRubles(roublesInput(minor))).toEqual({ ok: true, minor });
    }
    expect(roublesInput(19990)).toBe("199,90");
    expect(formatRoubles(129990)).toBe("1\u00a0299,90\u00a0₽");
    expect(formatRoubles(1)).toBe("0,01\u00a0₽");
  });
});
