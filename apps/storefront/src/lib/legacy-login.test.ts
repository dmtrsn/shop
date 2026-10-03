// @vitest-environment node
import { describe, expect, it } from "vitest";
import { getLegacyLoginHref } from "./legacy-login";

describe("legacy login navigation configuration", () => {
  it("uses an explicit browser navigation URL, including its fragment", () => {
    expect(getLegacyLoginHref({ NODE_ENV: "production", STOREFRONT_LEGACY_LOGIN_URL: " https://shop.example.test/legacy/#/login " }))
      .toBe("https://shop.example.test/legacy/#/login");
  });
  it("defaults only development to the local Go login page", () => {
    expect(getLegacyLoginHref({ NODE_ENV: "development" })).toBe("http://127.0.0.1:8080/#/login");
    expect(getLegacyLoginHref({ NODE_ENV: "test" })).toBeNull();
    expect(getLegacyLoginHref({})).toBeNull();
  });
  it("permits absent production configuration without failing a build", () => {
    expect(getLegacyLoginHref({ NODE_ENV: "production" })).toBeNull();
    expect(getLegacyLoginHref({ NODE_ENV: "production", STOREFRONT_LEGACY_LOGIN_URL: "  " })).toBeNull();
  });
  it.each(["/login", "//shop.test/login", "javascript:alert(1)", "data:text/html,login", "ftp://shop.test/login", "https://", "https://user:password@shop.test/", "https://shop.test/\nlogin", "https://shop.test\\evil", "not a url"])("rejects unsafe/malformed configuration: %s", (url) => {
    expect(() => getLegacyLoginHref({ STOREFRONT_LEGACY_LOGIN_URL: url })).toThrow("STOREFRONT_LEGACY_LOGIN_URL");
  });
  it("is completely independent of STOREFRONT_API_BASE_URL", () => {
    const environment = { NODE_ENV: "production", STOREFRONT_API_BASE_URL: "https://secret.internal:8080" };
    expect(getLegacyLoginHref(environment)).toBeNull();
    expect(getLegacyLoginHref({ ...environment, NODE_ENV: "development" })).toBe("http://127.0.0.1:8080/#/login");
    expect(getLegacyLoginHref({ ...environment, STOREFRONT_LEGACY_LOGIN_URL: "https://public.test/#/login" })).toBe("https://public.test/#/login");
  });
});
