import { act, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
const { replace, router, navigation } = vi.hoisted(() => {
  const replace = vi.fn();
  return { replace, router: { replace }, navigation: { pathname: "/" } };
});
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => navigation.pathname }));
import { LegacyHashBridge } from "./legacy-hash-bridge";

afterEach(() => { replace.mockReset(); navigation.pathname = "/"; window.history.replaceState(null, "", "/"); });

describe("hash bridge navigation", () => {
  it.each(["/", "/storefront-preview"])("passes a canonical unprefixed path to Next's basePath-aware router from %s", (base) => {
    window.history.replaceState(null, "", base + "#/cart");
    const { unmount } = render(<LegacyHashBridge />);
    expect(replace).toHaveBeenCalledExactlyOnceWith("/cart");
    act(() => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
    expect(replace).toHaveBeenCalledTimes(1);
    unmount();
    window.history.replaceState(null, "", base + "#/profile");
    act(() => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
    expect(replace).toHaveBeenCalledTimes(1);
  });
  it("ignores normal anchors and handles a later recognized hash", () => {
    window.history.replaceState(null, "", "/#main-content");
    render(<LegacyHashBridge />);
    expect(replace).not.toHaveBeenCalled();
    window.history.replaceState(null, "", "/#/orders");
    act(() => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
    expect(replace).toHaveBeenCalledExactlyOnceWith("/account/orders");
  });
  it.each([
    ["/", "/", "#/home"], ["/", "/", "#/"], ["/", "/", "#/delivery"], ["/cart", "/cart", "#/cart"],
    ["/storefront-preview/cart", "/cart", "#/cart"],
  ])("drops a stale legacy hash in place when already on the target (%s%s)", (url, pathname, hash) => {
    navigation.pathname = pathname;
    window.history.replaceState(null, "", url + hash);
    render(<LegacyHashBridge />);
    expect(replace).not.toHaveBeenCalled();
    expect(window.location.pathname + window.location.search + window.location.hash).toBe(url);
  });
  it("still navigates when only the query differs from the target", () => {
    navigation.pathname = "/catalog";
    window.history.replaceState(null, "", "/catalog#/catalog?sort=priceDesc");
    render(<LegacyHashBridge />);
    expect(replace).toHaveBeenCalledExactlyOnceWith("/catalog?sort=price_desc");
  });
});
