import { describe, expect, it, vi } from "vitest";

import { getStaffSession, loginStaff, logoutStaff, parseStaffSession, STAFF_SESSION_PATH } from "./staff-api";

const staff = { id: "stf_" + "a".repeat(32), email: "ops@example.com", displayName: "Оператор", createdAt: "2026-10-19T10:00:00Z" };

function respond(status: number, body?: unknown, headers: Record<string, string> = {}) {
  const payload = body === undefined ? null : typeof body === "string" ? body : JSON.stringify(body);
  return vi.fn<typeof fetch>().mockResolvedValue(new Response(payload, { status, headers }));
}

function sent(fetcher: ReturnType<typeof respond>) {
  const [url, init] = fetcher.mock.calls[0];
  return { url, init: init!, headers: new Headers(init!.headers) };
}

describe("staff session API client", () => {
  it("signs in with exactly email and password on the staff endpoint", async () => {
    const fetcher = respond(200, { staff, expiresAt: "2026-10-19T22:00:00Z", csrfToken: "csrf-1" });
    const result = await loginStaff("ops@example.com", "secret password", fetcher);
    const request = sent(fetcher);
    expect(request.url).toBe(STAFF_SESSION_PATH);
    expect(request.init.method).toBe("POST");
    expect(request.init.credentials).toBe("same-origin");
    expect(JSON.parse(request.init.body as string)).toEqual({ email: "ops@example.com", password: "secret password" });
    expect(request.headers.has("Authorization")).toBe(false);
    expect(result).toEqual({ status: "signed-in", staff: { id: staff.id, email: staff.email, displayName: staff.displayName }, csrfToken: "csrf-1" });
  });

  it.each([
    [401, { error: { code: "unauthorized" } }, {}, { status: "invalid" }],
    [422, { error: { code: "validation_error" } }, {}, { status: "invalid" }],
    [429, "Too many sign-in attempts. Try again later.\n", { "Retry-After": "12" }, { status: "rate-limited", retryAfterSeconds: 12 }],
    [429, "Too many", {}, { status: "rate-limited" }],
    [403, { error: { code: "csrf_failed" } }, {}, { status: "unavailable" }],
    [503, { error: { code: "service_unavailable" } }, { "Retry-After": "2" }, { status: "unavailable" }],
    [500, "<html>", {}, { status: "unavailable" }],
    [200, { staff: { id: "usr-demo" }, csrfToken: "x" }, {}, { status: "unavailable" }],
  ])("maps sign-in HTTP %i to one generic outcome", async (status, body, headers, expected) => {
    expect(await loginStaff("ops@example.com", "pw", respond(status, body, headers))).toEqual(expected);
  });

  it("treats a network failure as unavailable, never as invalid", async () => {
    expect(await loginStaff("a@b.c", "pw", vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline")))).toEqual({ status: "unavailable" });
    expect(await getStaffSession(vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline")))).toEqual({ status: "unavailable" });
  });

  it("reads the session from the staff cookie only", async () => {
    const fetcher = respond(200, { authenticated: true, staff, expiresAt: "2026-10-19T22:00:00Z", csrfToken: "csrf-2" });
    expect(await getStaffSession(fetcher)).toEqual({ status: "authenticated", staff: { id: staff.id, email: staff.email, displayName: staff.displayName }, csrfToken: "csrf-2" });
    const request = sent(fetcher);
    expect(request.init.method).toBe("GET");
    expect(request.init.cache).toBe("no-store");
    expect(request.headers.has("Authorization")).toBe(false);
    expect(await getStaffSession(respond(200, { authenticated: false }))).toEqual({ status: "anonymous" });
    expect(await getStaffSession(respond(502, "bad gateway"))).toEqual({ status: "unavailable" });
  });

  it("rejects malformed session bodies", () => {
    for (const body of [null, [], { authenticated: "yes" }, { authenticated: true }, { authenticated: true, staff, csrfToken: "" }, { authenticated: true, staff: { ...staff, id: "usr_1" }, csrfToken: "x" }]) {
      expect(parseStaffSession(body)).toEqual({ status: "unavailable" });
    }
  });

  it("signs out with the staff CSRF token and the expected staff ID, never customer headers", async () => {
    const fetcher = respond(204);
    expect(await logoutStaff("csrf-3", staff.id, fetcher)).toEqual({ status: "signed-out" });
    const request = sent(fetcher);
    expect(request.url).toBe(STAFF_SESSION_PATH);
    expect(request.init.method).toBe("DELETE");
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-3");
    expect(request.headers.get("X-Expected-Staff-ID")).toBe(staff.id);
    expect(request.headers.has("X-Expected-Authenticated-User-ID")).toBe(false);
    expect(request.headers.has("Authorization")).toBe(false);
  });

  it.each([
    [401, "expired"],
    [403, "stale-csrf"],
    [409, "changed"],
    [500, "unavailable"],
  ])("maps sign-out HTTP %i to %s", async (status, expected) => {
    expect(await logoutStaff("csrf", staff.id, respond(status, { error: { code: "x" } }))).toEqual({ status: expected });
  });
});
