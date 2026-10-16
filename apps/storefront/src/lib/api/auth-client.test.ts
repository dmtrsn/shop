import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { jsonResponse } from "@/test/favorites-fixtures";

import { confirmRegistration, registerCustomer, resendRegistration, startBrowserSession } from "./auth-client";
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

describe("self-registration calls", () => {
  const registration = { name: "Анна", email: "ann@example.test", password: "unit-test-only", phone: "" };

  it("registers once without CSRF or identity headers and omits an empty phone", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ status: "pending" }, 202, null));
    await expect(registerCustomer(registration)).resolves.toBeUndefined();
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/auth/registration", expect.objectContaining({
      method: "POST", credentials: "same-origin", cache: "no-store",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ name: "Анна", email: "ann@example.test", password: "unit-test-only" }),
    }));
    fetcher.mockResolvedValueOnce(jsonResponse({ status: "pending" }, 202, null));
    await registerCustomer({ ...registration, phone: "+7 900" });
    expect(JSON.parse(fetcher.mock.calls[1][1]!.body as string)).toMatchObject({ phone: "+7 900" });
  });

  it.each([
    ["200 instead of 202", { status: "pending" }, 200], ["another status", { status: "created" }, 202], ["not an object", ["pending"], 202],
  ])("rejects an unexpected registration answer (%s)", async (_, body, status) => {
    fetcher.mockResolvedValueOnce(jsonResponse(body, status, null));
    await expect(registerCustomer(registration)).rejects.toBeInstanceOf(BrowserRequestError);
  });

  it("surfaces Go field errors for registration", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "validation_error", message: "x", fields: [{ field: "password", message: "Короткий" }] } }, 422, null));
    await expect(registerCustomer(registration)).rejects.toMatchObject({ status: 422, fields: [{ field: "password", message: "Короткий" }] });
  });

  it("resends with the address only", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ status: "pending" }, 202, null));
    await resendRegistration("ann@example.test");
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/auth/registration/resend", expect.objectContaining({ method: "POST", body: JSON.stringify({ email: "ann@example.test" }) }));
  });

  it("confirms with the token in the POST body only and drops the session body", async () => {
    const token = "t".repeat(43);
    fetcher.mockResolvedValueOnce(jsonResponse(success, 200, null));
    await expect(confirmRegistration({ token, password: "unit-test-only" })).resolves.toBeUndefined();
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("/api/auth/registration/confirm");
    expect(String(url)).not.toContain(token);
    expect(init?.body).toBe(JSON.stringify({ token, password: "unit-test-only" }));
    fetcher.mockResolvedValueOnce(jsonResponse({ ...success, csrfToken: "" }, 200, null));
    await expect(confirmRegistration({ token, password: "unit-test-only" })).rejects.toBeInstanceOf(BrowserRequestError);
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "link_invalid", message: "x" } }, 410, null));
    await expect(confirmRegistration({ token, password: "unit-test-only" })).rejects.toMatchObject({ status: 410, code: "link_invalid" });
  });
});
