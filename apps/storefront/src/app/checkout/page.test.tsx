import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { FavoritesProvider } from "@/test/private-providers";
import { sessionResponse } from "@/test/favorites-fixtures";

const { getServerSession } = vi.hoisted(() => ({ getServerSession: vi.fn() }));
vi.mock("@/lib/api/session", () => ({ getServerSession }));
vi.mock("next/navigation", () => ({ usePathname: () => "/checkout", useRouter: () => ({ replace: vi.fn(), push: vi.fn() }) }));

import Page, { dynamic, metadata } from "./page";
import Loading from "./loading";
import ErrorPage from "./error";

beforeEach(() => { vi.clearAllMocks(); });

describe("checkout server boundary", () => {
  it("is dynamic, noindex and passes only status and user ID; no Cart or credentials in HTML", async () => {
    expect(dynamic).toBe("force-dynamic");
    expect(metadata.robots).toEqual({ index: false, follow: false });
    getServerSession.mockResolvedValue({ status: "authenticated", user: sessionResponse().user, csrfToken: "csrf-sensitive", token: "raw-cookie-sensitive" });
    const element = await Page();
    expect(element.props).toEqual({ initialSession: { status: "authenticated", userID: "A" } });
    const html = renderToStaticMarkup(<FavoritesProvider>{element}</FavoritesProvider>);
    for (const secret of ["csrf-sensitive", "raw-cookie-sensitive", "user@example.test"]) expect(html).not.toContain(secret);
    expect(html).toContain("Оформление заказа");
    expect(html).toContain("Загружаем заказ");
    expect(html).not.toContain("Адрес доставки");
  });

  it.each(["anonymous", "unavailable"])("preserves the %s gate", async (status) => {
    getServerSession.mockResolvedValue({ status, csrfToken: "should-not-pass" });
    const element = await Page();
    expect(element.props).toEqual({ initialSession: { status } });
    expect(renderToStaticMarkup(<FavoritesProvider>{element}</FavoritesProvider>)).not.toContain("should-not-pass");
  });

  it("has a stable loading state and an actionable error boundary without internals", () => {
    expect(renderToStaticMarkup(<Loading />)).toContain('aria-busy="true"');
    const html = renderToStaticMarkup(<ErrorPage error={new Error("internal secret")} reset={() => {}} />);
    expect(html).toContain("Повторить"); expect(html).not.toContain("internal secret");
  });
});
