import { describe, expect, it } from "vitest";

import { getStorefrontBasePath } from "./runtime-config";

describe("storefront runtime configuration", () => {
  it("keeps the local development storefront at the origin by default", () => {
    expect(getStorefrontBasePath({})).toBeUndefined();
    expect(getStorefrontBasePath({ STOREFRONT_BASE_PATH: "  " })).toBeUndefined();
  });

  it("accepts a clean, explicit preview path", () => {
    expect(getStorefrontBasePath({ STOREFRONT_BASE_PATH: " /storefront-preview " })).toBe("/storefront-preview");
  });

  it.each(["/", "storefront-preview", "/storefront-preview/", "/storefront?preview", "/a//b", "/a b"])(
    "rejects invalid base path %j",
    (STOREFRONT_BASE_PATH) => {
      expect(() => getStorefrontBasePath({ STOREFRONT_BASE_PATH })).toThrow(/STOREFRONT_BASE_PATH/);
    },
  );
});
