import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { FavoritesProvider } from "@/test/private-providers";
import { sessionResponse } from "@/test/favorites-fixtures";

const { getServerSession } = vi.hoisted(() => ({ getServerSession: vi.fn() }));
vi.mock("@/lib/api/session", () => ({ getServerSession }));

import { routeOrderID } from "@/lib/api/order-client";

import Page, { dynamic, metadata } from "./page";
import Loading from "./loading";
import ErrorPage from "./error";

beforeEach(() => { vi.clearAllMocks(); });

describe("order server boundary", () => {
  it("is dynamic, noindex and renders no order data on the server", async () => {
    expect(dynamic).toBe("force-dynamic");
    expect(metadata.robots).toEqual({ index: false, follow: false });
    getServerSession.mockResolvedValue({ status: "authenticated", user: sessionResponse().user, csrfToken: "csrf-sensitive" });
    const element = await Page({ params: Promise.resolve({ id: "ord-2026-10484" }) });
    expect(element.props).toEqual({ initialSession: { status: "authenticated", userID: "A" }, orderID: "ord-2026-10484" });
    const html = renderToStaticMarkup(<FavoritesProvider loginHref={null}>{element}</FavoritesProvider>);
    expect(html).not.toContain("csrf-sensitive");
    expect(html).toContain("Загружаем заказ");
  });

  it("decodes the route segment once for the client request", () => {
    expect(routeOrderID("%2310485")).toBe("#10485");
    expect(routeOrderID("ord-2026-10485")).toBe("ord-2026-10485");
    expect(routeOrderID("%E0%A4%A")).toBe("%E0%A4%A");
  });

  it("has a stable loading state and an actionable error boundary", () => {
    expect(renderToStaticMarkup(<Loading />)).toContain('aria-busy="true"');
    const html = renderToStaticMarkup(<ErrorPage error={new Error("internal secret")} reset={() => {}} />);
    expect(html).toContain("Повторить"); expect(html).not.toContain("internal secret");
  });
});
