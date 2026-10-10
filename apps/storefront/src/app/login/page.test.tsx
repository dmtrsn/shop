import { render, screen } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getServerSession, getDemoLoginAllowed, redirect } = vi.hoisted(() => ({
  getServerSession: vi.fn(),
  getDemoLoginAllowed: vi.fn(),
  redirect: vi.fn((target: string) => { throw Object.assign(new Error("NEXT_REDIRECT"), { target }); }),
}));
vi.mock("@/lib/api/session", () => ({ getServerSession, getDemoLoginAllowed }));
vi.mock("next/navigation", () => ({ redirect }));
// The client form is tested separately; here only the server-normalized target matters.
vi.mock("@/components/storefront/login-form", () => ({
  LoginForm: ({ target }: { target: string }) => <form data-target={target} />,
}));

import Page, { dynamic, metadata } from "./page";
import Loading from "./loading";

type Query = Record<string, string | string[] | undefined>;
const page = (query: Query = {}) => Page({ searchParams: Promise.resolve(query) });
const targetOf = (element: Awaited<ReturnType<typeof Page>>) => {
  const { container } = render(element);
  return container.querySelector("form")!.getAttribute("data-target");
};

beforeEach(() => {
  vi.clearAllMocks();
  getServerSession.mockResolvedValue({ status: "anonymous" });
  getDemoLoginAllowed.mockResolvedValue(false);
});

describe("/login server page", () => {
  it("is dynamic and not indexed", () => {
    expect(dynamic).toBe("force-dynamic");
    expect(metadata.robots).toEqual({ index: false, follow: false });
  });

  it.each([
    ["/checkout", "/checkout"], ["/account", "/account"], ["/account/orders/ord123", "/account/orders/ord123"],
    ["/account/orders/ord123/repeat", "/account/orders/ord123/repeat"], ["/cart?q=1#top", "/cart?q=1#top"],
  ])("renders the form for an anonymous visitor with target %s", async (returnTo, target) => {
    const element = await page({ returnTo });
    expect(targetOf(element)).toBe(target);
    expect(screen.getByRole("heading", { level: 1, name: "Вход в аккаунт" })).toBeInTheDocument();
    expect(screen.queryByRole("status")).toBeNull();
    expect(redirect).not.toHaveBeenCalled();
  });

  it.each([
    ["missing", {}], ["duplicate", { returnTo: ["/cart", "/checkout"] }], ["absolute", { returnTo: "https://evil.example/" }],
    ["scheme-relative", { returnTo: "//evil.example" }], ["backslash", { returnTo: "/\\evil.example" }],
    ["double-encoded", { returnTo: "/%255cevil.example" }], ["control", { returnTo: "/cart%0a" }],
    ["login loop", { returnTo: "/login?returnTo=%2Fcart" }], ["legacy", { returnTo: "/legacy/#/login" }],
    ["api", { returnTo: "/api/orders" }], ["_next", { returnTo: "/_next/static/x.js" }],
    ["credential query", { returnTo: "/cart?csrfToken=x" }], ["legacy profile hash", { returnTo: "/#/profile" }],
  ] as [string, Query][])("falls back to / for a %s returnTo", async (_, query) => {
    expect(targetOf(await page(query))).toBe("/");
  });

  it("redirects an authenticated visitor to the normalized target without showing the form", async () => {
    getServerSession.mockResolvedValue({ status: "authenticated", user: { id: "A", name: "N", email: "a@test.ru" } });
    await expect(page({ returnTo: "/account" })).rejects.toMatchObject({ target: "/account" });
    expect(redirect).toHaveBeenCalledExactlyOnceWith("/account");
    await expect(page({ returnTo: "https://evil.example/" })).rejects.toMatchObject({ target: "/" });
    await expect(page({ returnTo: ["/cart", "/cart"] })).rejects.toMatchObject({ target: "/" });
    expect(getDemoLoginAllowed).not.toHaveBeenCalled();
  });

  it("keeps the form when the session check is unavailable, with a neutral notice", async () => {
    getServerSession.mockResolvedValue({ status: "unavailable", requestId: "req-1" });
    const element = await page({ returnTo: "/checkout" });
    expect(targetOf(element)).toBe("/checkout");
    expect(screen.getByRole("status")).toHaveTextContent("Не удалось проверить текущий вход");
    expect(document.body.innerHTML).not.toContain("req-1");
    expect(redirect).not.toHaveBeenCalled();
  });

  it("shows the demo account only when Go explicitly advertises it", async () => {
    render(await page());
    expect(screen.queryByText(/Демо-режим/)).toBeNull();
    getDemoLoginAllowed.mockResolvedValue(true);
    const html = renderToStaticMarkup(await page());
    expect(html).toContain("Демо-режим");
    expect(html).toContain("demo@sugar.ru");
    expect(html).not.toContain('value="Sweet2026!"');
  });

  it("has a busy loading state", () => {
    expect(renderToStaticMarkup(<Loading />)).toContain('aria-busy="true"');
  });
});
