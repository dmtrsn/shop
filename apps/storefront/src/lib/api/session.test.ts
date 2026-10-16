// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { getAuthCapabilities, getDemoLoginAllowed, getServerSession, parseServerSession, sessionCookieHeader } from "./session";

// The raw incoming Cookie header, which (unlike a name-keyed map) can repeat names.
const cookieJar = vi.hoisted(() => ({ pairs: [] as [string, string][] }));

vi.mock("next/headers", () => ({
  headers: async () =>
    new Headers(cookieJar.pairs.length ? { cookie: cookieJar.pairs.map(([name, value]) => `${name}=${value}`).join("; ") } : {}),
}));

const SECURE = "__Host-sweetshop_session";
const DEVELOPMENT = "sweetshop_session";
const TOKEN = "Zm9vYmFyYmF6cXV4cXV1eGNvcmdlZ3JhdWx0Z2FycGx5";

const authenticatedBody = {
  authenticated: true,
  user: { id: "usr-demo", name: "Алексей", email: "demo@sugar.ru", phone: "+7 999", createdAt: "2025-11-04T10:00:00Z" },
  expiresAt: "2026-10-27T00:00:00Z",
  csrfToken: "csrf-secret-value",
  demoLogin: true,
  unexpected: "value",
};

function json(body: unknown, status = 200, requestId = "go-request") {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": requestId },
  });
}

const fetcher = vi.fn<typeof fetch>();

function sentCookie() {
  return new Headers(fetcher.mock.calls[0][1]?.headers).get("cookie");
}

beforeEach(() => {
  cookieJar.pairs.length = 0;
  fetcher.mockReset();
});

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("getServerSession", () => {
  it("is anonymous without contacting Go when no recognized cookie exists", async () => {
    cookieJar.pairs.push(["theme", "dark"]);
    cookieJar.pairs.push(["_ga", "GA1.1"]);
    cookieJar.pairs.push(["__Host-other_session", TOKEN]);

    await expect(getServerSession({ fetcher })).resolves.toEqual({ status: "anonymous" });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it.each([
    [SECURE, `${SECURE}=${TOKEN}`],
    [DEVELOPMENT, `${DEVELOPMENT}=${TOKEN}`],
  ])("forwards only the %s cookie to GET /api/auth/session with no-store", async (name, expected) => {
    cookieJar.pairs.push([name, TOKEN]);
    cookieJar.pairs.push(["theme", "dark"]);
    fetcher.mockResolvedValue(json({ authenticated: false, demoLogin: true }));

    await getServerSession({ fetcher });

    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0][0]).toBe("http://127.0.0.1:8080/api/auth/session");
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store" }));
    expect(sentCookie()).toBe(expected);
    const headers = new Headers(fetcher.mock.calls[0][1]?.headers);
    expect([...headers.keys()].sort()).toEqual(["accept", "cookie"]);
  });

  it.each([
    ["secure", SECURE],
    ["development", DEVELOPMENT],
  ])("forwards every occurrence of a duplicated %s cookie instead of choosing one", async (_label, name) => {
    cookieJar.pairs.push([name, "first-token"], ["theme", "dark"], [name, "second-token"]);
    // Go's session endpoint answers ambiguous duplicates as not signed in.
    fetcher.mockResolvedValue(json({ authenticated: false, demoLogin: true }));

    const session = await getServerSession({ fetcher });

    expect(sentCookie()).toBe(`${name}=first-token; ${name}=second-token`);
    expect(session).toEqual({ status: "anonymous" });
  });

  it("orders mixed duplicates: all secure occurrences, then all development occurrences", async () => {
    cookieJar.pairs.push(
      [DEVELOPMENT, "dev-1"],
      [SECURE, "secure-1"],
      ["_ga", "GA1.1"],
      [DEVELOPMENT, "dev-2"],
      [SECURE, "secure-2"],
    );
    fetcher.mockResolvedValue(json({ error: { code: "unauthorized", message: "x", requestId: "go-dup" } }, 401, "go-dup"));

    const session = await getServerSession({ fetcher });

    expect(sentCookie()).toBe(`${SECURE}=secure-1; ${SECURE}=secure-2; ${DEVELOPMENT}=dev-1; ${DEVELOPMENT}=dev-2`);
    expect(session).toEqual({ status: "unavailable", requestId: "go-dup" });
  });

  it("forwards both recognized cookies in a fixed order and lets Go decide", async () => {
    cookieJar.pairs.push([DEVELOPMENT, "dev-token"]);
    cookieJar.pairs.push(["theme", "dark"]);
    cookieJar.pairs.push([SECURE, "secure-token"]);
    fetcher.mockResolvedValue(json({ authenticated: false, demoLogin: false }));

    await getServerSession({ fetcher });

    expect(sentCookie()).toBe(`${SECURE}=secure-token; ${DEVELOPMENT}=dev-token`);
  });

  it("maps an authenticated response to exactly id, name and email", async () => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockResolvedValue(json(authenticatedBody));

    const session = await getServerSession({ fetcher });

    expect(session).toEqual({
      status: "authenticated",
      user: { id: "usr-demo", name: "Алексей", email: "demo@sugar.ru" },
    });
    const serialized = JSON.stringify(session);
    for (const leaked of ["csrf", "csrf-secret-value", "expiresAt", "demoLogin", "phone", "unexpected", TOKEN]) {
      expect(serialized).not.toContain(leaked);
    }
  });

  it("maps a confirmed signed-out response to anonymous", async () => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockResolvedValue(json({ authenticated: false, demoLogin: true }));

    await expect(getServerSession({ fetcher })).resolves.toEqual({ status: "anonymous" });
  });

  it.each([
    ["401", () => json({ error: { code: "unauthorized", message: "x", requestId: "go-401" } }, 401, "go-401"), "go-401"],
    ["403", () => json({ error: { code: "csrf_failed", message: "x", requestId: "go-403" } }, 403, "go-403"), "go-403"],
    ["500", () => json({ error: { code: "internal_error", message: "x", requestId: "go-500" } }, 500, "go-500"), "go-500"],
    ["502 without JSON", () => new Response("Bad gateway", { status: 502, headers: { "X-Request-ID": "edge-502" } }), "edge-502"],
    ["invalid JSON", () => new Response("{", { status: 200, headers: { "X-Request-ID": "go-bad-json" } }), "go-bad-json"],
    ["non-200 success", () => json({ authenticated: false }, 201, "go-201"), "go-201"],
    ["missing authenticated", () => json({ user: authenticatedBody.user }, 200, "go-shape"), "go-shape"],
    ["string authenticated", () => json({ authenticated: "false" }, 200, "go-shape"), "go-shape"],
    ["array body", () => json([], 200, "go-shape"), "go-shape"],
    ["authenticated without user", () => json({ authenticated: true, csrfToken: "x" }, 200, "go-user"), "go-user"],
    ["user without id", () => json({ authenticated: true, user: { name: "A", email: "a@b" } }, 200, "go-user"), "go-user"],
    ["empty user id", () => json({ authenticated: true, user: { id: "", name: "A", email: "a@b" } }, 200, "go-user"), "go-user"],
    ["numeric user id", () => json({ authenticated: true, user: { id: 1, name: "A", email: "a@b" } }, 200, "go-user"), "go-user"],
    ["user without email", () => json({ authenticated: true, user: { id: "u", name: "A" } }, 200, "go-user"), "go-user"],
  ])("maps %s to unavailable, never anonymous", async (_case, response, requestId) => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockResolvedValue(response());

    await expect(getServerSession({ fetcher })).resolves.toEqual({ status: "unavailable", requestId });
  });

  it("maps a network error to unavailable without exposing the cookie", async () => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockRejectedValue(new TypeError("fetch failed"));

    const session = await getServerSession({ fetcher });

    expect(session).toEqual({ status: "unavailable" });
    expect(JSON.stringify(session)).not.toContain(TOKEN);
  });

  it("maps a timeout to unavailable", async () => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockImplementation(
      (_input, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(init.signal?.reason));
        }),
    );

    await expect(getServerSession({ fetcher, timeoutMs: 10 })).resolves.toEqual({ status: "unavailable" });
  });

  it("is unavailable, never anonymous, when production lacks STOREFRONT_API_BASE_URL", async () => {
    vi.stubEnv("NODE_ENV", "production");
    vi.stubEnv("STOREFRONT_API_BASE_URL", "");
    cookieJar.pairs.push([SECURE, TOKEN]);

    await expect(getServerSession({ fetcher })).resolves.toEqual({ status: "unavailable" });
    expect(fetcher).not.toHaveBeenCalled();
  });
});

describe("session cookie header", () => {
  it("never splices values that are not valid cookie octets", () => {
    for (const value of ["", "a b", 'a"b', "a,b", "a\\b", "токен"]) {
      expect(sessionCookieHeader(`${SECURE}=${value}`)).toBeUndefined();
    }
    expect(sessionCookieHeader(`${SECURE}=a b; ${DEVELOPMENT}=${TOKEN}`)).toBe(`${DEVELOPMENT}=${TOKEN}`);
  });

  it("excludes unrelated and look-alike cookies and tolerates spacing", () => {
    expect(sessionCookieHeader(null)).toBeUndefined();
    expect(sessionCookieHeader("theme=dark; __Host-other=x; sweetshop_session_old=y; Sweetshop_session=z")).toBeUndefined();
    expect(sessionCookieHeader(` theme=dark ;${DEVELOPMENT}=${TOKEN} ; flag`)).toBe(`${DEVELOPMENT}=${TOKEN}`);
  });
});

describe("parseServerSession", () => {
  it("omits requestId when none is known", () => {
    expect(parseServerSession({ nope: true })).toEqual({ status: "unavailable" });
  });
});

describe("getDemoLoginAllowed", () => {
  it("is true only for an explicit demoLogin: true and never forwards browser cookies", async () => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockResolvedValueOnce(json({ authenticated: false, demoLogin: true }));
    await expect(getDemoLoginAllowed({ fetcher })).resolves.toBe(true);
    expect(fetcher).toHaveBeenCalledOnce();
    expect(String(fetcher.mock.calls[0][0])).toMatch(/\/api\/auth\/session$/);
    expect(sentCookie()).toBeNull();
  });

  it.each([
    ["false", { authenticated: false, demoLogin: false }], ["missing", { authenticated: false }],
    ["truthy string", { authenticated: false, demoLogin: "true" }], ["not an object", [true]],
  ])("is false when the flag is %s", async (_, body) => {
    fetcher.mockResolvedValueOnce(json(body));
    await expect(getDemoLoginAllowed({ fetcher })).resolves.toBe(false);
  });

  it("is false when Go fails or is unreachable", async () => {
    fetcher.mockResolvedValueOnce(json({ error: { code: "internal_error" } }, 500));
    await expect(getDemoLoginAllowed({ fetcher })).resolves.toBe(false);
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    await expect(getDemoLoginAllowed({ fetcher })).resolves.toBe(false);
  });
});

describe("getAuthCapabilities", () => {
  it("reports selfService and demoLogin only when Go sends exactly true, without browser cookies", async () => {
    cookieJar.pairs.push([SECURE, TOKEN]);
    fetcher.mockResolvedValueOnce(json({ authenticated: false, demoLogin: false, selfService: true }));
    await expect(getAuthCapabilities({ fetcher })).resolves.toEqual({ status: "available", demoLogin: false, selfService: true });
    expect(sentCookie()).toBeNull();
    fetcher.mockResolvedValueOnce(json({ authenticated: false, demoLogin: true, selfService: "true" }));
    await expect(getAuthCapabilities({ fetcher })).resolves.toEqual({ status: "available", demoLogin: true, selfService: false });
    fetcher.mockResolvedValueOnce(json({ authenticated: false }));
    await expect(getAuthCapabilities({ fetcher })).resolves.toEqual({ status: "available", demoLogin: false, selfService: false });
  });

  it("is unavailable, not disabled, when Go fails, is unreachable or answers nonsense", async () => {
    fetcher.mockResolvedValueOnce(json({ error: { code: "internal_error" } }, 500));
    await expect(getAuthCapabilities({ fetcher })).resolves.toEqual({ status: "unavailable" });
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    await expect(getAuthCapabilities({ fetcher })).resolves.toEqual({ status: "unavailable" });
    fetcher.mockResolvedValueOnce(json([true]));
    await expect(getAuthCapabilities({ fetcher })).resolves.toEqual({ status: "unavailable" });
  });
});
