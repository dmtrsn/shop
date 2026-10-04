import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
const { getServerSession } = vi.hoisted(() => ({ getServerSession: vi.fn() }));
vi.mock("@/lib/api/session", () => ({ getServerSession }));
import Page, { dynamic, metadata } from "./page";
import Loading from "./loading";
import ErrorPage from "./error";

describe("repeat route boundary", () => {
  it("passes only sanitized dynamic session and decoded order identity", async () => {
    getServerSession.mockResolvedValue({ status: "authenticated", user: { id: "A" }, csrfToken: "secret" });
    const page = await Page({ params: Promise.resolve({ id: "%2310482" }) });
    expect(page.props).toEqual({ orderID: "#10482", initialSession: { status: "authenticated", userID: "A" } });
    expect(dynamic).toBe("force-dynamic"); expect(metadata.robots).toEqual({ index: false, follow: false });
  });
  it.each(["anonymous", "unavailable"])("keeps %s gate", async (status) => {
    getServerSession.mockResolvedValue({ status });
    expect((await Page({ params: Promise.resolve({ id: "ord-1" }) })).props.initialSession).toEqual({ status });
  });
  it("has one h1 for loading/error without private error details or unsafe automatic retry", () => {
    for (const element of [<Loading key="loading" />, <ErrorPage key="error" />]) expect(renderToStaticMarkup(element).match(/<h1/g)).toHaveLength(1);
  });
  it("Next repeat transport contains only strict v2 paths; entry links never prefetch previews", () => {
    const client = readFileSync("src/lib/api/repeat-client.ts", "utf8");
    expect(client).toContain("/api/v2/orders/"); expect(client).not.toContain("/api/orders/");
    for (const file of ["orders-view.tsx", "order-view.tsx"]) {
      const source = readFileSync(`src/components/storefront/${file}`, "utf8");
      expect(source).toMatch(/\/repeat`} prefetch={false}/);
      expect(source).not.toContain("previewBrowserRepeat");
    }
  });
});
