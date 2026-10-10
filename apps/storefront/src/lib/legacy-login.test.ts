// @vitest-environment node
import { describe, expect, it } from "vitest";
import { getLegacyLoginHref } from "./legacy-login";

describe("same-origin legacy login configuration", () => {
  it.each(["production", "development", "test"])("has a usable default in %s", (NODE_ENV) => {
    expect(getLegacyLoginHref({ NODE_ENV })).toBe("/legacy/#/login");
    expect(getLegacyLoginHref({ NODE_ENV, STOREFRONT_LEGACY_LOGIN_URL: " " })).toBe("/legacy/#/login");
  });
  it("accepts the explicit canonical entry", () => {
    expect(getLegacyLoginHref({ STOREFRONT_LEGACY_LOGIN_URL: " /legacy/#/login " })).toBe("/legacy/#/login");
  });
  it.each(["https://shop.test/legacy/#/login", "//evil.test/legacy/#/login", "javascript:alert(1)", "data:text/html,x",
    "/legacy/?returnTo=//evil.test#/login", "/login", "/legacy/#/profile", "/legacy\\/#/login", "/legacy/%0a#/login"])("rejects cross-origin or noncanonical entry %s", (STOREFRONT_LEGACY_LOGIN_URL) => {
    expect(() => getLegacyLoginHref({ STOREFRONT_LEGACY_LOGIN_URL })).toThrow();
  });
  it("does not derive browser URLs from the internal API", () => {
    expect(getLegacyLoginHref({ NODE_ENV: "production", STOREFRONT_API_BASE_URL: "http://secret.internal:8080" } as Parameters<typeof getLegacyLoginHref>[0])).toBe("/legacy/#/login");
  });
});
