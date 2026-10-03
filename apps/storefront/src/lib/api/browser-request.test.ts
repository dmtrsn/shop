import { describe, expect, it, vi } from "vitest";

import { jsonResponse } from "@/test/favorites-fixtures";

import { browserRequest, BrowserRequestError, ResponseOwnerError } from "./browser-request";

function request(response: Response, expectedUserID = "A") {
  return browserRequest("/api/cart", { expectedUserID, fetcher: vi.fn().mockResolvedValue(response) });
}

describe("private response owner binding", () => {
  it("accepts a body bound to the expected user", async () => {
    await expect(request(jsonResponse({ ok: true }, 200, "A"))).resolves.toEqual({ ok: true });
  });

  it.each([
    [200, "B"], [200, null], [200, ""], [201, null],
    // Downstream errors are produced after authentication and must be bound too.
    [404, null], [409, "B"], [422, null], [500, "B"],
  ])("fails closed for %s with owner %j", async (status, owner) => {
    await expect(request(jsonResponse({ error: { code: "x" } }, status, owner))).rejects.toBeInstanceOf(ResponseOwnerError);
  });

  it("an empty expected owner never matches an empty header", async () => {
    await expect(request(jsonResponse({}, 200, ""), "")).rejects.toBeInstanceOf(ResponseOwnerError);
  });

  it.each([
    [401, "unauthorized"], [403, "csrf_failed"],
  ])("pre-authentication %s carries no owner and keeps its code", async (status, code) => {
    const result = request(jsonResponse({ error: { code } }, status, null));
    await expect(result).rejects.not.toBeInstanceOf(ResponseOwnerError);
    await expect(result).rejects.toMatchObject({ status, code });
  });

  it("an unbound 5xx (session lookup failure) is an uncertain error, not private data", async () => {
    const result = request(jsonResponse({ error: { code: "internal_error" } }, 500, null));
    await expect(result).rejects.toBeInstanceOf(BrowserRequestError);
    await expect(result).rejects.not.toBeInstanceOf(ResponseOwnerError);
  });

  it("ignores spoofable request input: only the response header is compared", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({}, 200, "A"));
    await browserRequest("/api/cart", { expectedUserID: "A", fetcher });
    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect(init.headers).not.toHaveProperty("X-Authenticated-User-ID");
    expect(init.headers).not.toHaveProperty("Authorization");
    expect(init).toMatchObject({ credentials: "same-origin", cache: "no-store" });
  });
});
