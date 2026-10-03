import { describe, expect, it, vi } from "vitest";
import { favoriteProduct, favoriteResponse, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { FavoritesRequestError, getBrowserFavorites, getBrowserSession, parseBrowserSession, setBrowserFavorite } from "./favorites-client";

describe("browser favorites transport", () => {
  it("retains only user ID and in-memory CSRF from authenticated session data", () => {
    expect(parseBrowserSession({ ...sessionResponse(), expiresAt: "date", token: "never-return-this" }))
      .toEqual({ status: "authenticated", userID: "A", csrfToken: "csrf-A" });
    expect(parseBrowserSession({ authenticated: false })).toEqual({ status: "anonymous" });
  });
  it.each([null, {}, { authenticated: true }, { authenticated: true, user: { id: "A" }, csrfToken: "" }, { authenticated: "false" }])("malformed sessions are unavailable (%#)", (body) => {
    expect(parseBrowserSession(body)).toEqual({ status: "unavailable" });
  });
  it.each([401, 500, 502])("session HTTP %s is unavailable, never anonymous", async (status) => {
    expect(await getBrowserSession({ fetcher: vi.fn().mockResolvedValue(jsonResponse({}, status)) })).toEqual({ status: "unavailable" });
  });
  it("session network failures and timeouts are unavailable", async () => {
    expect(await getBrowserSession({ fetcher: vi.fn().mockRejectedValue(new TypeError("network")) })).toEqual({ status: "unavailable" });
    const fetcher: typeof fetch = (_url, options) => new Promise((_resolve, reject) => {
      options?.signal?.addEventListener("abort", () => reject(new Error("timeout")));
    });
    expect(await getBrowserSession({ fetcher, timeoutMs: 5 })).toEqual({ status: "unavailable" });
  });
  it("GETs only the same-origin favorites representation", async () => {
    const body = favoriteResponse(favoriteProduct());
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(body));
    expect(await getBrowserFavorites({ fetcher })).toEqual(body);
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/favorites", expect.objectContaining({
      method: "GET", credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" },
    }));
  });
  it.each([[true, "PUT"], [false, "DELETE"]] as const)("mutation enabled=%s has exact CSRF header, no body or credentials in headers", async (enabled, method) => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(favoriteResponse()));
    await setBrowserFavorite("id/with spaces", enabled, "current-csrf", { fetcher });
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/favorites/id%2Fwith%20spaces", expect.objectContaining({
      method, credentials: "same-origin", cache: "no-store",
      headers: { Accept: "application/json", "X-CSRF-Token": "current-csrf" },
    }));
    const options = fetcher.mock.calls[0][1];
    expect(options).not.toHaveProperty("body");
    expect(options.headers).not.toHaveProperty("Authorization");
    expect(options.headers).not.toHaveProperty("Cookie");
    expect(options.headers).not.toHaveProperty("Origin");
  });
  it("preserves csrf_failed and does not retry within transport", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ error: { code: "csrf_failed" } }, 403));
    await expect(setBrowserFavorite("p", true, "old-csrf", { fetcher })).rejects.toMatchObject({ status: 403, code: "csrf_failed" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("preserves a 401 even when the error body is not JSON", async () => {
    await expect(getBrowserFavorites({ fetcher: vi.fn().mockResolvedValue(new Response("Unauthorized", { status: 401 })) })).rejects.toMatchObject({ status: 401 });
  });
  it("rejects an invalid successful representation", async () => {
    await expect(setBrowserFavorite("p", true, "csrf", { fetcher: vi.fn().mockResolvedValue(jsonResponse({ items: [] })) })).rejects.toThrow();
  });
  it("a mutation timeout is an uncertain request failure, with no retry", async () => {
    const fetcher = vi.fn<typeof fetch>().mockImplementation((_url, options) => new Promise((_resolve, reject) => {
      options?.signal?.addEventListener("abort", () => reject(new Error("timeout")));
    }));
    await expect(setBrowserFavorite("p", true, "csrf", { fetcher, timeoutMs: 5 })).rejects.toBeInstanceOf(FavoritesRequestError);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});
