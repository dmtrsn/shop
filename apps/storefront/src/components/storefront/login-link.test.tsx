import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const navigation = vi.hoisted(() => ({ pathname: "/checkout" as string | null }));
vi.mock("next/navigation", () => ({ usePathname: () => navigation.pathname }));

import { LoginLink } from "./login-link";

afterEach(() => { window.history.replaceState(null, "", "/"); vi.unstubAllGlobals(); navigation.pathname = "/checkout"; });
const href = () => screen.getByRole("link", { name: "Войти" }).getAttribute("href")!;

describe("Next sign-in link", () => {
  it("links signed-out /checkout to /login?returnTo=%2Fcheckout without any request", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    window.history.replaceState(null, "", "/checkout");
    render(<LoginLink>Войти</LoginLink>);
    expect(href()).toBe("/login?returnTo=%2Fcheckout");
    expect(fetch).not.toHaveBeenCalled();
  });

  it.each(["/favorites", "/cart", "/account", "/account/orders", "/account/orders/ord123", "/account/orders/ord123/repeat", "/products/chocolate"])("captures %s with its query and fragment", (path) => {
    navigation.pathname = path;
    window.history.replaceState(null, "", path + "?tab=items#details");
    render(<LoginLink>Войти</LoginLink>);
    const url = new URL(href(), window.location.origin);
    expect(url.pathname).toBe("/login");
    expect(url.searchParams.get("returnTo")).toBe(path + "?tab=items#details");
    expect(href()).not.toContain("legacy");
  });

  it("uses the app pathname, not the basePath-prefixed browser path", () => {
    navigation.pathname = "/cart";
    window.history.replaceState(null, "", "/storefront-preview/cart?q=1");
    render(<LoginLink>Войти</LoginLink>);
    expect(href()).toBe("/login?returnTo=%2Fcart%3Fq%3D1");
  });

  it("follows fragment changes", () => {
    window.history.replaceState(null, "", "/checkout");
    render(<LoginLink>Войти</LoginLink>);
    act(() => {
      window.history.replaceState(null, "", "/checkout#delivery");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(href()).toBe("/login?returnTo=%2Fcheckout%23delivery");
  });

  it("falls back to home for a target the contract rejects", () => {
    navigation.pathname = "/cart";
    window.history.replaceState(null, "", "/cart?csrfToken=leak");
    render(<LoginLink>Войти</LoginLink>);
    expect(href()).toBe("/login?returnTo=%2F");
  });

  it("passes presentation props through", () => {
    render(<LoginLink aria-label="Войдите, чтобы купить: Шоколад" className="x" title="t">Войти</LoginLink>);
    const link = screen.getByRole("link", { name: "Войдите, чтобы купить: Шоколад" });
    expect(link).toHaveClass("x");
    expect(link).toHaveAttribute("title", "t");
  });
});
