import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";

import { createBrowserSession, parseBrowserSession } from "./browser-session";

const fetcher = vi.fn<typeof fetch>();
function respond(body: unknown, status = 200) { fetcher.mockResolvedValueOnce(jsonResponse(body, status, null)); }

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("shared browser session", () => {
  it("projects only validated account fields from the same session", () => {
    const body = { ...sessionResponse(), user: { id: "A", name: "Анна", email: "anna@test.ru", phone: "+7 123", birthday: "secret", createdAt: "date" }, token: "secret" };
    expect(parseBrowserSession(body)).toEqual({ status: "authenticated", userID: "A", csrfToken: "csrf-A",
      account: { name: "Анна", email: "anna@test.ru", phone: "+7 123" } });
    for (const phone of [undefined, "", "   "]) {
      expect(parseBrowserSession({ ...body, user: { ...body.user, phone } })).toEqual({
        status: "authenticated", userID: "A", csrfToken: "csrf-A", account: { name: "Анна", email: "anna@test.ru" },
      });
    }
    expect(parseBrowserSession({ ...body, authenticated: false })).toEqual({ status: "anonymous" });
  });

  it.each([{ name: null }, { name: 7 }, { email: undefined }, { email: [] }, { phone: null }, { phone: 123 }])(
    "rejects malformed account fields (%j)", (fields) => {
      const body = sessionResponse();
      expect(parseBrowserSession({ ...body, user: { ...body.user, ...fields } })).toEqual({ status: "unavailable" });
    },
  );

  it("replaces every A field atomically, including dropping A's optional phone", async () => {
    const a = sessionResponse();
    respond({ ...a, user: { ...a.user, phone: "only-A" } });
    const store = createBrowserSession();
    await store.ensure();
    const snapshots: unknown[] = [];
    store.subscribe(() => snapshots.push(store.get()));
    respond({ ...sessionResponse("B"), user: { id: "B", name: "B name", email: "b@test.ru" } });
    await store.refresh();
    expect(snapshots).toEqual([
      { status: "loading", generation: 2 },
      { status: "authenticated", userID: "B", csrfToken: "csrf-B", generation: 2, account: { name: "B name", email: "b@test.ru" } },
    ]);
  });

  it("starts idle and issues no request until a feature asks", () => {
    const store = createBrowserSession();
    expect(store.get()).toEqual({ status: "idle", generation: 0 });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("authenticates once with memory-only CSRF and same-origin credentials", async () => {
    respond(sessionResponse());
    const store = createBrowserSession();
    await store.ensure();
    expect(store.get()).toEqual({ status: "authenticated", userID: "A", csrfToken: "csrf-A", account: { name: "Покупатель", email: "user@example.test" }, generation: 1 });
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/auth/session", expect.objectContaining({
      method: "GET", credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" },
    }));
  });

  it("anonymous is distinct from unavailable", async () => {
    respond({ authenticated: false, demoLogin: false });
    const store = createBrowserSession();
    await store.ensure();
    expect(store.get()).toEqual({ status: "anonymous", generation: 1 });
  });

  it.each([
    ["5xx", () => respond({ error: { code: "internal_error" } }, 500)],
    ["network", () => fetcher.mockRejectedValueOnce(new TypeError("offline"))],
    ["malformed", () => respond({ authenticated: true, user: { id: "A" } })],
  ])("%s session checks are unavailable, never anonymous", async (_name, arrange) => {
    arrange();
    const store = createBrowserSession();
    await store.ensure();
    expect(store.get()).toEqual({ status: "unavailable", generation: 1 });
  });

  it("deduplicates initialization and explicit refresh", async () => {
    const first = deferred<Response>();
    fetcher.mockReturnValueOnce(first.promise);
    const store = createBrowserSession();
    const a = store.ensure();
    expect(store.ensure()).toBe(a);
    expect(store.refresh()).toBe(a);
    expect(fetcher).toHaveBeenCalledTimes(1);
    first.resolve(jsonResponse(sessionResponse()));
    await a;
    await expect(store.ensure()).resolves.toBeUndefined();
    expect(fetcher).toHaveBeenCalledTimes(1);

    const second = deferred<Response>();
    fetcher.mockReturnValueOnce(second.promise);
    const c = store.refresh();
    expect(store.refresh()).toBe(c);
    // Refresh hides the previous identity and its CSRF synchronously.
    expect(store.get()).toEqual({ status: "loading", generation: 2 });
    second.resolve(jsonResponse({ authenticated: false }));
    await c;
    expect(store.get()).toEqual({ status: "anonymous", generation: 2 });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it.each([
    ["anonymous", { authenticated: false }, { status: "anonymous" }],
    ["unavailable", { malformed: true }, { status: "unavailable" }],
    ["B", sessionResponse("B"), { status: "authenticated", userID: "B", csrfToken: "csrf-B", account: { name: "Покупатель", email: "user@example.test" } }],
  ])("A -> %s replaces identity with a new generation", async (_name, body, expected) => {
    respond(sessionResponse());
    const store = createBrowserSession();
    await store.ensure();
    const listener = vi.fn();
    store.subscribe(listener);
    respond(body);
    await store.refresh();
    expect(store.get()).toEqual({ ...expected, generation: 2 });
    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("an obsolete session check cannot resurrect an invalidated identity", async () => {
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    const store = createBrowserSession();
    const request = store.ensure();
    store.invalidate("anonymous");
    pending.resolve(jsonResponse(sessionResponse()));
    await request;
    expect(store.get()).toEqual({ status: "anonymous", generation: 2 });
  });

  it("a new server route identity supersedes an older check; repeats share it", async () => {
    const old = deferred<Response>();
    fetcher.mockReturnValueOnce(old.promise);
    const store = createBrowserSession();
    void store.ensure();
    respond(sessionResponse("B"));
    const route = store.enterRoute({ status: "authenticated", userID: "B" });
    expect(store.enterRoute({ status: "authenticated", userID: "B" })).toBe(route);
    await route;
    old.resolve(jsonResponse(sessionResponse("A")));
    await Promise.resolve();
    expect(store.get()).toMatchObject({ status: "authenticated", userID: "B" });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("anonymous/unavailable route gates hide identity without a request", () => {
    const store = createBrowserSession();
    void store.enterRoute({ status: "unavailable" });
    expect(store.get()).toEqual({ status: "unavailable", generation: 1 });
    void store.enterRoute({ status: "anonymous" });
    expect(store.get()).toEqual({ status: "anonymous", generation: 2 });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("owner recovery refreshes once, then settles unavailable instead of looping", async () => {
    respond(sessionResponse());
    const store = createBrowserSession();
    await store.ensure();
    respond(sessionResponse());
    await store.recoverOwner(1);
    expect(store.get()).toMatchObject({ status: "authenticated", generation: 2 });
    // A second mismatch in the recovered generation does not refresh again.
    await store.recoverOwner(2);
    expect(store.get()).toEqual({ status: "unavailable", generation: 3 });
    expect(fetcher).toHaveBeenCalledTimes(2);
    // A deliberate refresh may recover once more.
    respond(sessionResponse());
    await store.refresh();
    respond(sessionResponse());
    await store.recoverOwner(4);
    expect(store.get()).toMatchObject({ status: "authenticated", generation: 5 });
  });

  it("obsolete-generation owner failures are ignored", async () => {
    respond(sessionResponse());
    const store = createBrowserSession();
    await store.ensure();
    respond(sessionResponse("B"));
    await store.refresh();
    await store.recoverOwner(1);
    expect(store.get()).toMatchObject({ status: "authenticated", userID: "B", generation: 2 });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("never writes credentials or identity to browser storage", async () => {
    const local = vi.spyOn(Storage.prototype, "setItem");
    respond(sessionResponse());
    const store = createBrowserSession();
    await store.ensure();
    await store.refresh().catch(() => {});
    expect(local).not.toHaveBeenCalled();
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
    expect(document.cookie).not.toContain("csrf");
  });
});
