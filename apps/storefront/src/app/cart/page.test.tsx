import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { FavoritesProvider } from "@/test/private-providers";
import { favoriteProduct, sessionResponse } from "@/test/favorites-fixtures";

const { getServerSession } = vi.hoisted(() => ({ getServerSession: vi.fn() }));
vi.mock("@/lib/api/session", () => ({ getServerSession }));

import Page from "./page";
import Loading from "./loading";
import ErrorPage from "./error";

beforeEach(() => { vi.clearAllMocks(); });

describe("cart server boundary", () => {
  it("passes only authenticated status and user ID; never serializes tokens or raw session fields", async () => {
    getServerSession.mockResolvedValue({
      status: "authenticated", user: sessionResponse().user, csrfToken: "csrf-sensitive", token: "raw-cookie-sensitive",
      expiresAt: "private-expiry", demoLogin: true, apiOrigin: "http://internal:8080",
    });
    const element = await Page();
    expect(element.props).toEqual({ initialSession: { status: "authenticated", userID: "A" } });
    const html = renderToStaticMarkup(<FavoritesProvider loginHref={null}>{element}</FavoritesProvider>);
    for (const secret of ["csrf-sensitive", "raw-cookie-sensitive", "private-expiry", "http://internal", "user@example.test", favoriteProduct().name]) {
      expect(html).not.toContain(secret);
      expect(JSON.stringify(element.props)).not.toContain(secret);
    }
    expect(html).toContain("Загружаем корзину");
  });
  it.each(["anonymous", "unavailable"])("preserves the %s gate without extra session fields", async (status) => {
    getServerSession.mockResolvedValue({ status, requestId: "id", csrfToken: "should-not-pass" });
    const element = await Page();
    expect(element.props).toEqual({ initialSession: { status } });
    const html = renderToStaticMarkup(<FavoritesProvider loginHref={null}>{element}</FavoritesProvider>);
    expect(html).toContain(status === "anonymous" ? "Войдите" : "временно недоступна");
    expect(html).not.toContain("should-not-pass");
  });
  it("has layout-stable loading and an actionable unexpected error boundary", () => {
    expect(renderToStaticMarkup(<Loading />)).toContain('aria-busy="true"');
    const html = renderToStaticMarkup(<ErrorPage error={new Error("internal secret")} reset={() => {}} />);
    expect(html).toContain("Повторить"); expect(html).not.toContain("internal secret");
  });
});
