import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, favoriteProduct, favoriteResponse, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { FavoriteButton } from "./favorite-button";
import { FavoritesProvider, useFavorites } from "@/test/private-providers";

const p1 = favoriteProduct("p01");
const p2 = favoriteProduct("p02");
const fetcher = vi.fn<typeof fetch>();
function respond(body: unknown, status = 200, owner = "A") { fetcher.mockResolvedValueOnce(jsonResponse(body, status, owner)); }
function fail(status: number, code: string) { respond({ error: { code } }, status); }
function requests() { return fetcher.mock.calls.map(([path, options]) => `${options?.method} ${path}`); }
function Probe({ initialize = true }: { initialize?: boolean }) {
  const state = useFavorites();
  useEffect(() => { if (initialize) void state.ensure(); }, [initialize, state.ensure]);
  return <>
    <button onClick={() => void state.refresh()}>Refresh session</button>
    <button onClick={() => state.enterRoute({ status: "authenticated", userID: "B" })}>Enter B route</button>
    <button onClick={() => state.enterRoute({ status: "anonymous" })}>Enter anonymous route</button>
    <button onClick={() => { void state.toggle("p01"); void state.toggle("p02"); }}>Two synchronous writes</button>
    <div data-testid="state">{JSON.stringify({ status: state.status, userID: state.userID, ids: state.favorites?.items.map((p) => p.id) ?? null })}</div>
  </>;
}
function mount({ controls = true, initialize = true, strict = false } = {}) {
  const tree = <FavoritesProvider loginHref={null}><Probe initialize={initialize} />{controls ? <>
    <FavoriteButton productID="p01" productName="First" />
    <FavoriteButton productID="p01" productName="First copy" />
    <FavoriteButton productID="p02" productName="Second" />
  </> : null}</FavoritesProvider>;
  return render(strict ? <StrictMode>{tree}</StrictMode> : tree);
}
function view() { return JSON.parse(screen.getByTestId("state").textContent!); }
async function ready(ids: string[], userID = "A") {
  await waitFor(() => expect(view()).toEqual({ status: "ready", userID, ids }));
}
function refresh() { fireEvent.click(screen.getByText("Refresh session")); }
function first() { return screen.getByRole("button", { name: "Избранное: First" }); }

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("lazy shared initialization", () => {
  it("does not fetch merely because the provider is mounted", () => {
    mount({ controls: false, initialize: false });
    expect(fetcher).not.toHaveBeenCalled();
    expect(view().status).toBe("idle");
  });
  it("deduplicates controls, including React effect replay, to two requests", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    mount({ strict: true });
    await ready(["p01"]);
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/favorites"]);
    expect(first()).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Избранное: First copy" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Избранное: Second" })).toHaveAttribute("aria-pressed", "false");
  });
  it.each(["anonymous", "unavailable"] as const)("%s performs no favorites GET", async (status) => {
    respond(status === "anonymous" ? { authenticated: false } : { malformed: true });
    mount();
    await waitFor(() => expect(view().status).toBe(status));
    expect(view().ids).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session"]);
    expect(screen.queryByRole("button", { pressed: false })).not.toBeInTheDocument();
  });
});

describe("authenticated snapshot ownership", () => {
  it.each(["anonymous", "unavailable"] as const)("A -> %s clears the private snapshot", async (status) => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    mount(); await ready(["p01"]);
    respond(status === "anonymous" ? { authenticated: false } : { error: {} }, status === "anonymous" ? 200 : 500);
    refresh();
    expect(view().ids).toBeNull();
    await waitFor(() => expect(view()).toEqual({ status, userID: null, ids: null }));
    expect(requests()).toHaveLength(3);
  });
  it("A -> B never renders A's favorites while B's list is loading", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    mount(); await ready(["p01"]);
    const bList = deferred<Response>();
    respond(sessionResponse("B")); fetcher.mockReturnValueOnce(bList.promise);
    refresh();
    expect(view().ids).toBeNull();
    await waitFor(() => expect(view().userID).toBe("B"));
    expect(view().ids).toBeNull();
    await act(async () => bList.resolve(jsonResponse(favoriteResponse(p2), 200, "B")));
    await ready(["p02"], "B");
  });
  it("ignores A's late GET after switching to B even if abort is ignored", async () => {
    const aList = deferred<Response>();
    respond(sessionResponse()); fetcher.mockReturnValueOnce(aList.promise);
    mount(); await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    respond(sessionResponse("B")); respond(favoriteResponse(p2), 200, "B");
    refresh(); await ready(["p02"], "B");
    await act(async () => aList.resolve(jsonResponse(favoriteResponse(p1))));
    expect(view()).toEqual({ status: "ready", userID: "B", ids: ["p02"] });
  });
  it.each(["success", "401", "csrf_failed"])("ignores A's late mutation %s after switching to B", async (outcome) => {
    respond(sessionResponse()); respond(favoriteResponse());
    mount(); await ready([]);
    const write = deferred<Response>(); fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(first());
    respond(sessionResponse("B")); respond(favoriteResponse(p2), 200, "B");
    refresh(); await ready(["p02"], "B");
    const response = outcome === "success" ? jsonResponse(favoriteResponse(p1))
      : jsonResponse({ error: { code: outcome === "401" ? "unauthorized" : "csrf_failed" } }, outcome === "401" ? 401 : 403);
    await act(async () => write.resolve(response));
    expect(view()).toEqual({ status: "ready", userID: "B", ids: ["p02"] });
    expect(requests()).toHaveLength(5);
  });
  it("a later same-user recovery reloads a fresh snapshot", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    mount(); await ready(["p01"]);
    fetcher.mockRejectedValueOnce(new TypeError("offline")); refresh();
    await waitFor(() => expect(view().status).toBe("unavailable"));
    expect(view().ids).toBeNull();
    const recovered = deferred<Response>();
    respond(sessionResponse()); fetcher.mockReturnValueOnce(recovered.promise); refresh();
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(5));
    expect(view().ids).toBeNull();
    await act(async () => recovered.resolve(jsonResponse(favoriteResponse(p2))));
    await ready(["p02"]);
  });
  it("ignores a late reconciliation result after the user changes", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    mount(); await ready(["p01"]);
    const reconciliation = deferred<Response>();
    fail(500, "internal_error"); fetcher.mockReturnValueOnce(reconciliation.promise);
    fireEvent.click(first()); await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    respond(sessionResponse("B")); respond(favoriteResponse(p2), 200, "B"); refresh();
    await ready(["p02"], "B");
    await act(async () => reconciliation.resolve(jsonResponse(favoriteResponse(p1))));
    expect(view().ids).toEqual(["p02"]);
  });
  it.each(["B", "anonymous"])("ignores late A CSRF recovery after entering the %s route", async (destination) => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    mount(); await ready(["p01"]);
    const recovery = deferred<Response>();
    fail(403, "csrf_failed"); fetcher.mockReturnValueOnce(recovery.promise);
    fireEvent.click(first()); await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    if (destination === "B") { respond(sessionResponse("B")); respond(favoriteResponse(p2), 200, "B"); }
    fireEvent.click(screen.getByText(`Enter ${destination} route`));
    expect(view().ids).toBeNull();
    if (destination === "B") await ready(["p02"], "B");
    else expect(view().status).toBe("anonymous");
    const expected = view();
    await act(async () => recovery.resolve(jsonResponse(sessionResponse("A", "late-csrf"))));
    expect(view()).toEqual(expected);
    expect(requests()).toHaveLength(destination === "B" ? 6 : 4);
    expect(requests().filter((r) => r.startsWith("DELETE"))).toHaveLength(1);
  });
});

describe("confirmed mutations and recovery", () => {
  it("adds/removes non-optimistically and reconciles the complete response across copies", async () => {
    respond(sessionResponse()); respond(favoriteResponse()); mount(); await ready([]);
    const write = deferred<Response>(); fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(first());
    expect(first()).toHaveAttribute("aria-pressed", "false");
    expect(first()).toHaveAttribute("aria-disabled", "true");
    expect(view().ids).toEqual([]);
    await act(async () => write.resolve(jsonResponse(favoriteResponse(p2, p1))));
    await ready(["p02", "p01"]);
    expect(screen.getByRole("button", { name: "Избранное: First copy" })).toHaveAttribute("aria-pressed", "true");
    respond(favoriteResponse(p2)); fireEvent.click(first()); await ready(["p02"]);
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/favorites", "PUT /api/favorites/p01", "DELETE /api/favorites/p01"]);
  });
  it("serializes the whole aggregate, ignores repeated clicks, and never queues intents", async () => {
    respond(sessionResponse()); respond(favoriteResponse()); mount(); await ready([]);
    const write = deferred<Response>(); fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(screen.getByText("Two synchronous writes"));
    fireEvent.click(first()); fireEvent.click(screen.getByRole("button", { name: "Избранное: Second" }));
    expect(requests()).toHaveLength(3);
    await act(async () => write.resolve(jsonResponse(favoriteResponse(p1))));
    await ready(["p01"]); expect(requests()).toHaveLength(3);
  });
  it("csrf_failed refreshes session and list once, with no write replay", async () => {
    respond(sessionResponse()); respond(favoriteResponse()); mount(); await ready([]);
    fail(403, "csrf_failed"); respond(sessionResponse("A", "new-csrf")); respond(favoriteResponse());
    fireEvent.click(first());
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(5)); await ready([]);
    expect(screen.getByRole("status", { name: "Уведомления избранного" })).toHaveTextContent("Действие не выполнено");
    expect(requests().filter((r) => r.startsWith("PUT"))).toHaveLength(1);
    respond(favoriteResponse(p1)); fireEvent.click(first()); await ready(["p01"]);
    expect(fetcher.mock.calls[5][1]?.headers).toEqual({ Accept: "application/json", "X-CSRF-Token": "new-csrf" });
  });
  it.each(["anonymous", "unavailable"] as const)("csrf_failed followed by %s discards the private list", async (status) => {
    respond(sessionResponse()); respond(favoriteResponse(p1)); mount(); await ready(["p01"]);
    fail(403, "csrf_failed"); respond(status === "anonymous" ? { authenticated: false } : {});
    fireEvent.click(first()); await waitFor(() => expect(view().status).toBe(status));
    expect(view().ids).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/favorites", "DELETE /api/favorites/p01", "GET /api/auth/session"]);
  });
  it.each(["read", "write"])("protected %s 401 immediately signs out without retry", async (operation) => {
    respond(sessionResponse());
    if (operation === "read") fail(401, "unauthorized");
    else respond(favoriteResponse(p1));
    mount();
    if (operation === "write") { await ready(["p01"]); fail(401, "unauthorized"); fireEvent.click(first()); }
    await waitFor(() => expect(view()).toEqual({ status: "anonymous", userID: null, ids: null }));
    expect(requests()).toHaveLength(operation === "read" ? 2 : 3);
  });
  it.each(["network", "timeout", "500", "malformed success"])("%s reconciles with only a GET and never claims rollback", async (failure) => {
    respond(sessionResponse()); respond(favoriteResponse()); mount(); await ready([]);
    if (failure === "network") fetcher.mockRejectedValueOnce(new TypeError("network"));
    else if (failure === "timeout") fetcher.mockRejectedValueOnce(new DOMException("timeout", "TimeoutError"));
    else if (failure === "500") fail(500, "internal_error");
    else respond({ invalid: true });
    const read = deferred<Response>(); fetcher.mockReturnValueOnce(read.promise);
    fireEvent.click(first());
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    expect(view().ids).toBeNull();
    expect(screen.getByRole("status", { name: "Уведомления избранного" })).toHaveTextContent("Не удалось подтвердить изменение");
    await act(async () => read.resolve(jsonResponse(favoriteResponse(p1))));
    await ready(["p01"]);
    expect(requests().slice(2)).toEqual(["PUT /api/favorites/p01", "GET /api/favorites"]);
  });
  it("a failed reconciliation hides stale state and blocks another mutation until recovery", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1)); mount(); await ready(["p01"]);
    fail(500, "internal_error"); fail(500, "internal_error"); fireEvent.click(first());
    await waitFor(() => expect(view().status).toBe("unavailable"));
    expect(view().ids).toBeNull();
    fireEvent.click(screen.getByText("Two synchronous writes"));
    expect(fetcher).toHaveBeenCalledTimes(4);
    respond(sessionResponse()); respond(favoriteResponse()); refresh(); await ready([]);
  });
  it("404 reconciles a removed product; out-of-stock products remain removable", async () => {
    respond(sessionResponse()); respond(favoriteResponse({ ...p1, stock: 0 })); mount(); await ready(["p01"]);
    fail(404, "not_found"); respond(favoriteResponse()); fireEvent.click(first()); await ready([]);
    expect(screen.getByRole("status", { name: "Уведомления избранного" })).toHaveTextContent("Товар больше недоступен");
    expect(requests().slice(2)).toEqual(["DELETE /api/favorites/p01", "GET /api/favorites"]);
  });
  it("a same-user visibility refresh reads only after the active network write settles", async () => {
    respond(sessionResponse()); respond(favoriteResponse()); mount(); await ready([]);
    const write = deferred<Response>(); fetcher.mockReturnValueOnce(write.promise); fireEvent.click(first());
    respond(sessionResponse()); respond(favoriteResponse(p1)); refresh();
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    expect(view().ids).toBeNull();
    await act(async () => write.resolve(jsonResponse(favoriteResponse(p1))));
    await ready(["p01"]);
    expect(requests()).toHaveLength(5);
    expect(screen.getByRole("status", { name: "Уведомления избранного" })).not.toHaveTextContent("Сохраняем избранное");
  });
  it("unmounting controls leaves the provider's write alive across navigation", async () => {
    respond(sessionResponse()); respond(favoriteResponse());
    const { rerender } = mount(); await ready([]);
    const write = deferred<Response>(); fetcher.mockReturnValueOnce(write.promise); fireEvent.click(first());
    rerender(<FavoritesProvider loginHref={null}><Probe /></FavoritesProvider>);
    await act(async () => write.resolve(jsonResponse(favoriteResponse(p1)))); await ready(["p01"]);
    expect(requests()).toHaveLength(3);
  });
});

describe("lifecycle and storage", () => {
  it("revalidates once after a hidden document returns and coalesces restored events", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1)); mount(); await ready(["p01"]);
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    fireEvent(document, new Event("visibilitychange"));
    const session = deferred<Response>(); fetcher.mockReturnValueOnce(session.promise); respond(favoriteResponse(p2));
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
    fireEvent(document, new Event("visibilitychange"));
    fireEvent(window, new PageTransitionEvent("pageshow", { persisted: true }));
    expect(fetcher).toHaveBeenCalledTimes(3);
    await act(async () => session.resolve(jsonResponse(sessionResponse()))); await ready(["p02"]);
    expect(fetcher).toHaveBeenCalledTimes(4);
  });
  it("does not read cookie credentials or use browser storage", async () => {
    const cookie = vi.spyOn(document, "cookie", "get");
    const get = vi.spyOn(Storage.prototype, "getItem");
    const set = vi.spyOn(Storage.prototype, "setItem");
    respond(sessionResponse()); respond(favoriteResponse()); mount(); await ready([]);
    respond(favoriteResponse(p1)); fireEvent.click(first()); await ready(["p01"]);
    expect(cookie).not.toHaveBeenCalled(); expect(get).not.toHaveBeenCalled(); expect(set).not.toHaveBeenCalled();
    expect(screen.getByTestId("state")).not.toHaveTextContent("csrf");
  });
});
