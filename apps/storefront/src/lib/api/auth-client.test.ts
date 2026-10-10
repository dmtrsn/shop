import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { jsonResponse } from "@/test/favorites-fixtures";

import { startBrowserSession } from "./auth-client";
import { BrowserRequestError } from "./browser-request";

const fetcher = vi.fn<typeof fetch>();
const credentials = { email: "user@example.test", password: "unit-test-only" };
const success = { user: { id: "A", name: "Покупатель", email: "user@example.test" }, expiresAt: "2026-11-01T00:00:00Z", csrfToken: "csrf-login-secret" };

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("startBrowserSession", () => {
  it("sends exactly one same-origin, no-store cookie-login POST and returns nothing from the body", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse(success, 200, null));
    await expect(startBrowserSession(credentials)).resolves.toBeUndefined();
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/auth/session", expect.objectContaining({
      method: "POST", credentials: "same-origin", cache: "no-store",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify(credentials),
    }));
  });

  it.each([
    ["no user", { ...success, user: undefined }],
    ["empty user id", { ...success, user: { ...success.user, id: "" } }],
    ["numeric user id", { ...success, user: { ...success.user, id: 7 } }],
    ["no CSRF token", { ...success, csrfToken: undefined }],
    ["blank CSRF token", { ...success, csrfToken: "  " }],
    ["not an object", ["A"]],
  ])("rejects a malformed success body (%s)", async (_, body) => {
    fetcher.mockResolvedValueOnce(jsonResponse(body, 200, null));
    await expect(startBrowserSession(credentials)).rejects.toBeInstanceOf(BrowserRequestError);
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it.each([
    [401, "unauthorized"], [403, "csrf_failed"], [422, "validation_error"], [500, "internal_error"], [503, "unavailable"],
  ])("surfaces %i once, without retrying", async (status, code) => {
    fetcher.mockResolvedValue(jsonResponse({ error: { code, message: "detail" } }, status, null));
    await expect(startBrowserSession(credentials)).rejects.toMatchObject({ status, code });
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("does not retry a network failure", async () => {
    fetcher.mockRejectedValue(new TypeError("offline"));
    await expect(startBrowserSession(credentials)).rejects.toMatchObject({ status: undefined });
    expect(fetcher).toHaveBeenCalledOnce();
  });
});
