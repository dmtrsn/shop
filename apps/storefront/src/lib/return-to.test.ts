// @vitest-environment node
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";
import { buildLoginHref, loginReturnTarget, normalizeReturnTo } from "./return-to";

const origin = "https://shop.test";
type Migration = {
  normalizeReturnTo: typeof normalizeReturnTo;
  returnAfterLogin: (location: { search: string; origin: string; replace: (path: string) => void }) => boolean;
};
const browser = { SloyMigration: undefined as unknown as Migration };
runInNewContext(readFileSync(new URL("../../../../web/migration.js", import.meta.url), "utf8"), { window: browser, URL, URLSearchParams });
const legacy = browser.SloyMigration;

describe.each([["Next", normalizeReturnTo], ["legacy", legacy.normalizeReturnTo]] as const)("%s returnTo contract", (_, normalize) => {
  it.each(["/", "/cart", "/checkout", "/account/orders", "/account/orders/ord123?tab=items#details", "/storefront-preview/checkout?step=review", "/catalog?q=%D1%88%D0%BE%D0%BA%D0%BE%D0%BB%D0%B0%D0%B4"])("accepts %s unchanged", (path) => {
    expect(normalize(path, origin)).toBe(path);
  });
  it.each([
    "http://evil.example/", "https://evil.example/", "//evil.example", "javascript:alert(1)", "data:text/html,evil",
    "https://shop.test/cart", "cart", "", "/\\evil.example", "/%5cevil.example", "/%2f%2fevil.example",
    "/%255cevil.example", "/cart\n", "/cart%0a", "/cart%00", "/cart%", "/cart%XY", "/cart%C0%AF",
    "/legacy", "/legacy/", "/legacy/a#/login", "/l%65gacy/", "/x/../legacy/", "/login", "/storefront-preview/login",
    "/api", "/api/auth/session", "/_next/static/chunk.js", "/storefront-preview/_next/data/x.json", "/#login", "/#/login", "/#/profile", "/cart?csrfToken=secret", "/cart?userID=A",
    "/cart?returnTo=https://evil.example", "/" + "a".repeat(2048),
    "/x/..//evil.example", "/%2e%2e//evil.example", "/x/..//evil.example/cart",
  ])("rejects %s", (path) => { expect(normalize(path, origin)).toBeNull(); });
  it("normalizes dot segments", () => { expect(normalize("/catalog/../cart?q=1", origin)).toBe("/cart?q=1"); });
});

describe("Next login target", () => {
  it.each(["/favorites", "/cart", "/checkout", "/account", "/account/orders", "/account/orders/ord123", "/account/orders/ord123/repeat",
    "/account/orders/ord123?tab=items#details", "/catalog?q=%D1%88%D0%BE%D0%BA%D0%BE%D0%BB%D0%B0%D0%B4&page=2"])("keeps %s, including query and fragment", (target) => {
    expect(loginReturnTarget(target)).toBe(target);
    const url = new URL(buildLoginHref(target), origin);
    expect(url.pathname).toBe("/login");
    expect([...url.searchParams.keys()]).toEqual(["returnTo"]);
    expect(url.searchParams.get("returnTo")).toBe(target);
    expect(url.hash).toBe("");
  });
  it("encodes the route as the only query value", () => {
    expect(buildLoginHref("/checkout")).toBe("/login?returnTo=%2Fcheckout");
    expect(buildLoginHref("/cart?q=1#top")).toBe("/login?returnTo=%2Fcart%3Fq%3D1%23top");
  });
  it.each([
    ["absolute", "https://evil.example/"], ["scheme-relative", "//evil.example"], ["backslash", "/\\evil.example"],
    ["encoded backslash", "/%5cevil.example"], ["double-encoded", "/%255cevil.example"], ["malformed", "/cart%"],
    ["control", "/cart%0a"], ["dot-segment //", "/x/..//evil.example"], ["login loop", "/login?returnTo=%2Fcart"],
    ["auth", "/auth/x"], ["legacy", "/legacy/"], ["api", "/api/auth/session"], ["_next", "/_next/static/x.js"],
    ["csrf", "/cart?csrfToken=x"], ["token", "/cart?token=x"], ["password", "/cart?password=x"], ["user id", "/cart?userID=A"],
    ["expected user", "/cart?expectedUserID=A"], ["legacy login hash", "/#/login"], ["legacy profile hash", "/#/profile"],
  ])("falls back to / for %s targets", (_, target) => {
    expect(loginReturnTarget(target)).toBe("/");
    expect(buildLoginHref(target)).toBe("/login?returnTo=%2F");
  });
  it.each([[undefined], [null], [7], [["/cart", "/checkout"]], [["/cart"]]])("accepts only one string value (%j)", (candidate) => {
    expect(loginReturnTarget(candidate)).toBe("/");
  });
});

// web/migration.js stays as the /legacy rollback; its post-login return is still exercised.
describe("legacy rollback return", () => {
  it("returns with replace; missing target preserves the existing flow", () => {
    const replace = vi.fn();
    expect(legacy.returnAfterLogin({ origin, search: "", replace })).toBe(false);
    expect(replace).not.toHaveBeenCalled();
    expect(legacy.returnAfterLogin({ origin, search: "?returnTo=%2Fstorefront-preview%2Fcart%3Fq%3D1", replace })).toBe(true);
    expect(replace).toHaveBeenLastCalledWith("/storefront-preview/cart?q=1");
  });
  it.each(["?returnTo=https://evil.example", "?returnTo=%2Flegacy%2F", "?returnTo=%2Fcart&returnTo=%2Fcheckout"])("falls back harmlessly for %s", (search) => {
    const replace = vi.fn();
    legacy.returnAfterLogin({ origin, search, replace });
    expect(replace).toHaveBeenCalledExactlyOnceWith("/");
  });
});
