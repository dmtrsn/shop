import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LegacyLoginLink } from "./legacy-login-link";

afterEach(() => { window.history.replaceState(null, "", "/"); vi.unstubAllGlobals(); });

describe("shared sign-in link", () => {
  it.each(["/favorites", "/cart", "/checkout", "/account", "/account/orders", "/account/orders/ord123", "/account/orders/ord123/repeat"])("captures the preview location for %s without any session request", (path) => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const target = "/storefront-preview" + path + "?tab=items#details";
    window.history.replaceState(null, "", target);
    render(<LegacyLoginLink href="/legacy/#/login">Войти</LegacyLoginLink>);
    const url = new URL(screen.getByRole("link", { name: "Войти" }).getAttribute("href")!, window.location.origin);
    expect(url.searchParams.get("returnTo")).toBe(target);
    expect(url.pathname).toBe("/legacy/");
    expect(url.hash).toBe("#/login");
    expect(fetch).not.toHaveBeenCalled();
  });
  it("captures root deployment paths naturally", () => {
    window.history.replaceState(null, "", "/cart?q=1");
    render(<LegacyLoginLink href="/legacy/#/login">Войти</LegacyLoginLink>);
    expect(screen.getByRole("link").getAttribute("href")).toBe("/legacy/?returnTo=%2Fcart%3Fq%3D1#/login");
  });
});
