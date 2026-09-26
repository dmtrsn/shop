import { describe, expect, it } from "vitest";

import { productCountText } from "./plural";

describe("Russian product count", () => {
  it.each([
    [0, "0 товаров"],
    [1, "1 товар"],
    [2, "2 товара"],
    [5, "5 товаров"],
    [11, "11 товаров"],
    [21, "21 товар"],
    [112, "112 товаров"],
    [124, "124 товара"],
  ])("formats %i", (count, expected) => {
    expect(productCountText(count)).toBe(expected);
  });
});
