// @vitest-environment node
import { describe, expect, it, vi } from "vitest";

vi.mock("next/headers", () => ({ headers: vi.fn() }));

import { checkStaffSession, staffCookieHeader } from "./server-session";

const staff = { id: "stf_" + "b".repeat(32), email: "ops@example.com", displayName: "Оператор", createdAt: "2026-10-19T10:00:00Z" };
const options = (fetcher: typeof fetch) => ({ fetcher, baseUrl: "http://app:8080" });

describe("server staff session gate", () => {
  it("relays only staff cookies, never the customer session", () => {
    expect(staffCookieHeader("__Host-sweetshop_session=customer; __Host-sweetshop_staff=staff1; theme=dark")).toBe("__Host-sweetshop_staff=staff1");
    expect(staffCookieHeader("sweetshop_staff=dev; __Host-sweetshop_staff=a; __Host-sweetshop_staff=b")).toBe(
      "__Host-sweetshop_staff=a; __Host-sweetshop_staff=b; sweetshop_staff=dev",
    );
    expect(staffCookieHeader("__Host-sweetshop_session=customer")).toBeUndefined();
    expect(staffCookieHeader('__Host-sweetshop_staff=bad"value')).toBeUndefined();
    expect(staffCookieHeader(null)).toBeUndefined();
  });

  it("is anonymous without asking Go when no staff cookie is present", async () => {
    const fetcher = vi.fn<typeof fetch>();
    expect(await checkStaffSession("__Host-sweetshop_session=customer", options(fetcher))).toEqual({ status: "anonymous" });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("returns only the identity Go confirms, never the CSRF token", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(Response.json({ authenticated: true, staff, expiresAt: "x", csrfToken: "secret-csrf" }));
    const session = await checkStaffSession("__Host-sweetshop_staff=tok", options(fetcher));
    expect(session).toEqual({ status: "authenticated", staff: { id: staff.id, email: staff.email, displayName: staff.displayName } });
    expect(JSON.stringify(session)).not.toContain("secret-csrf");
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("http://app:8080/api/admin/v1/auth/session");
    expect(new Headers(init!.headers).get("Cookie")).toBe("__Host-sweetshop_staff=tok");
    expect(init!.cache).toBe("no-store");
  });

  it("never mistakes a broken backend for signed out", async () => {
    expect(await checkStaffSession("__Host-sweetshop_staff=tok", options(vi.fn<typeof fetch>().mockResolvedValue(Response.json({ authenticated: false }))))).toEqual({ status: "anonymous" });
    for (const fetcher of [
      vi.fn<typeof fetch>().mockResolvedValue(new Response("oops", { status: 500 })),
      vi.fn<typeof fetch>().mockResolvedValue(new Response("{", { status: 200 })),
      vi.fn<typeof fetch>().mockRejectedValue(new TypeError("connection refused")),
    ]) {
      expect(await checkStaffSession("__Host-sweetshop_staff=tok", options(fetcher))).toEqual({ status: "unavailable" });
    }
  });
});
