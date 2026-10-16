import { describe, expect, it } from "vitest";

import { initials } from "./initials";

describe("initials", () => {
  it.each([
    ["Алексей Морозов", "АМ"],
    ["анна", "А"],
    ["  Мария   Петровна  Иванова ", "МИ"],
    ["jane doe", "JD"],
    ["Ёлка", "Ё"],
    ["😀 Smile", "😀S"],
    ["", ""],
    ["   ", ""],
  ])("%j → %j", (name, expected) => {
    expect(initials(name)).toBe(expected);
  });
});
