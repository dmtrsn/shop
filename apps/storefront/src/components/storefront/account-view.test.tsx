import type Link from "next/link";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode, type ComponentProps } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { BrowserSessionStore, RouteSession } from "@/lib/api/browser-session";
import { deferred, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { PrivateProviders } from "@/test/private-providers";

import { AccountView } from "./account-view";
import { useBrowserSessionStore } from "./browser-session-provider";

vi.mock("next/link", () => ({ default: ({ href, children, prefetch }: ComponentProps<typeof Link>) =>
  <a href={String(href)} data-prefetch={String(prefetch)}>{children}</a> }));

const fetcher = vi.fn<typeof fetch>();
let store: BrowserSessionStore;
function Probe() { store = useBrowserSessionStore(); return null; }
function tree(initialSession: RouteSession) {
  return <StrictMode><PrivateProviders>
    <Probe /><AccountView initialSession={initialSession} />
  </PrivateProviders></StrictMode>;
}
function mount(initialSession: RouteSession = { status: "authenticated", userID: "A" }) {
  return render(tree(initialSession));
}
function account(userID = "A", phone?: string) {
  return { ...sessionResponse(userID), user: { id: userID, name: `Name ${userID}`, email: `${userID}@test.ru`, ...(phone !== undefined ? { phone } : {}) } };
}
function empty(owner: string | null = "A") {
  return new Response(null, { status: 204, headers: owner === null ? {} : { "X-Authenticated-User-ID": owner } });
}
function requests() { return fetcher.mock.calls.map(([path, init]) => `${init?.method} ${path}`); }
async function ready(userID = "A") { await screen.findByText(`Name ${userID}`); }
async function start() { fetcher.mockResolvedValueOnce(jsonResponse(account())); mount(); await ready(); }
function logout() { fireEvent.click(screen.getByRole("button", { name: "Выйти" })); }

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("account summary", () => {
  it("shows only real fields and passive links without initializing other features", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse(account("A", "+7 123")));
    mount();
    await ready();
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Аккаунт");
    expect(screen.getAllByRole("term").map((node) => node.textContent)).toEqual(["Имя", "Email", "Телефон"]);
    expect(screen.getAllByRole("definition").map((node) => node.textContent)).toEqual(["Name A", "A@test.ru", "+7 123"]);
    const links = within(screen.getByRole("navigation", { name: "Разделы аккаунта" })).getAllByRole("link");
    expect(links.map((link) => [link.textContent, link.getAttribute("href")])).toEqual([
      ["Мои заказы", "/account/orders"], ["Избранное", "/favorites"], ["Корзина", "/cart"],
    ]);
    for (const link of links) expect(link).toHaveAttribute("data-prefetch", "false");
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByText(/бонус|день рождения|редактировать|пароль|настройки|адреса/i)).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session"]);
  });

  it.each([undefined, "", "  "])("omits an empty optional phone (%j)", async (phone) => {
    fetcher.mockResolvedValueOnce(jsonResponse(account("A", phone)));
    mount(); await ready();
    expect(screen.queryByText("Телефон")).toBeNull();
  });

  it("shows no previous identity while checking and offers explicit retry when unavailable", async () => {
    await start();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    act(() => { void store.refresh(); });
    expect(screen.queryByText("Name A")).toBeNull();
    expect(within(screen.getByRole("main")).getByRole("status")).toHaveAttribute("aria-busy", "true");
    await act(async () => { pending.resolve(jsonResponse({ broken: true })); });
    expect(screen.getByText("Аккаунт временно недоступен")).toBeVisible();
    expect(screen.queryByText("Вы вышли из аккаунта")).toBeNull();
    fetcher.mockResolvedValueOnce(jsonResponse(account("B")));
    fireEvent.click(screen.getByRole("button", { name: "Повторить проверку" }));
    await ready("B");
  });

  it.each(["anonymous", "unavailable"] as const)("respects the %s server gate without browser reads", async (status) => {
    mount({ status });
    if (status === "anonymous") expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login?returnTo=%2F");
    else expect(screen.queryByRole("link", { name: "Войти" })).toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("signed out offers the Next sign-in page and an explicit recheck", async () => {
    mount({ status: "anonymous" });
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login?returnTo=%2F");
    fetcher.mockResolvedValueOnce(jsonResponse(account()));
    fireEvent.click(screen.getByRole("button", { name: "Проверить вход" }));
    await ready();
  });
});

describe("guarded logout", () => {
  it("waits for an owner-bound 204, deduplicates synchronously, clears shared data and focuses signed-out heading", async () => {
    await start();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    const button = screen.getByRole("button", { name: "Выйти" });
    act(() => { button.click(); button.click(); });
    expect(requests()).toEqual(["GET /api/auth/session", "DELETE /api/auth/session"]);
    expect(screen.getByText("Name A")).toBeVisible();
    expect(store.get().status).toBe("authenticated");
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute("aria-busy", "true");
    await act(async () => { pending.resolve(empty()); });
    expect(store.get()).toEqual({ status: "anonymous", generation: 2 });
    expect(screen.queryByText("Name A")).toBeNull();
    expect(screen.getByRole("heading", { name: "Вы вышли из аккаунта" })).toHaveFocus();
    expect(requests()).toHaveLength(2);
  });

  const failures: [string, () => Promise<Response>][] = [
    ["401", async () => jsonResponse({ error: { code: "unauthorized" } }, 401, null)],
    ["csrf", async () => jsonResponse({ error: { code: "csrf_failed" } }, 403, null)],
    ["409 owner change", async () => jsonResponse({ error: { code: "authenticated_user_changed" } }, 409, "B")],
    ["missing owner", async () => empty(null)],
    ["wrong owner", async () => empty("B")],
    ["network", async () => { throw new TypeError("offline"); }],
    ["timeout", async () => { throw new DOMException("timeout", "TimeoutError"); }],
    ["500", async () => jsonResponse({ error: { code: "internal_error" } }, 500, null)],
    ["malformed", async () => new Response("broken", { status: 200, headers: { "X-Authenticated-User-ID": "A" } })],
    ["unexpected 200", async () => jsonResponse({ ok: true })],
  ];
  describe.each(failures)("%s failure", (_name, failure) => {
    it.each(["authenticated", "anonymous", "unavailable"])("reconciles once to %s, never replays DELETE", async (outcome) => {
      await start();
      const reconciliation = deferred<Response>();
      fetcher.mockImplementationOnce(failure).mockReturnValueOnce(reconciliation.promise);
      logout();
      await waitFor(() => expect(requests()).toHaveLength(3));
      expect(store.get()).toEqual({ status: "loading", generation: 2 });
      expect(screen.queryByText("Name A")).toBeNull();
      await act(async () => { reconciliation.resolve(jsonResponse(outcome === "authenticated" ? account("B")
        : outcome === "anonymous" ? { authenticated: false } : { broken: true })); });
      expect(store.get().status).toBe(outcome);
      expect(requests()).toEqual(["GET /api/auth/session", "DELETE /api/auth/session", "GET /api/auth/session"]);
      if (outcome === "authenticated") {
        expect(screen.getByText("Name B")).toBeVisible();
        expect(within(screen.getByRole("main")).getByRole("status")).toHaveTextContent("Выход не подтверждён");
        expect(screen.getByRole("button", { name: "Выйти" })).toBeEnabled();
      } else if (outcome === "anonymous") expect(screen.getByRole("heading", { name: "Вы вышли из аккаунта" })).toHaveFocus();
      else {
        expect(within(screen.getByRole("main")).getByRole("status")).toHaveTextContent("не подтверждён");
        expect(screen.queryByText("Вы вышли из аккаунта")).toBeNull();
      }
    });
  });

  it("a fresh explicit click uses reconciled CSRF and identity, and may reconcile once again", async () => {
    await start();
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "csrf_failed" } }, 403, null))
      .mockResolvedValueOnce(jsonResponse(account("B")));
    logout(); await ready("B");
    fetcher.mockResolvedValueOnce(empty("C")).mockResolvedValueOnce(jsonResponse(account("C")));
    logout(); await ready("C");
    expect(fetcher.mock.calls[3][1]?.headers).toEqual({ Accept: "application/json", "X-Expected-Authenticated-User-ID": "B", "X-CSRF-Token": "csrf-B" });
    expect(requests().filter((r) => r.startsWith("DELETE"))).toHaveLength(2);
    expect(requests().filter((r) => r.startsWith("GET"))).toHaveLength(3);
  });

  it.each([true, false])("stale A completion (success=%s) cannot sign out B or publish feedback", async (success) => {
    await start();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    logout();
    fetcher.mockResolvedValueOnce(jsonResponse(account("B")));
    await act(async () => { await store.refresh(); });
    await ready("B");
    await act(async () => { if (success) pending.resolve(empty()); else pending.reject(new TypeError("offline")); });
    expect(store.get()).toMatchObject({ status: "authenticated", userID: "B" });
    expect(screen.queryByText("Вы вышли из аккаунта")).toBeNull();
    expect(within(screen.getByRole("main")).getByRole("status")).toBeEmptyDOMElement();
    expect(requests()).toHaveLength(3);
  });

  it("ignores a recovery result superseded by another identity generation", async () => {
    await start();
    const recovery = deferred<Response>();
    fetcher.mockRejectedValueOnce(new TypeError("offline")).mockReturnValueOnce(recovery.promise);
    logout(); await waitFor(() => expect(requests()).toHaveLength(3));
    fetcher.mockResolvedValueOnce(jsonResponse(account("B")));
    await act(async () => { await store.enterRoute({ status: "authenticated", userID: "B" }); });
    await act(async () => { recovery.resolve(jsonResponse({ authenticated: false })); });
    expect(store.get()).toMatchObject({ status: "authenticated", userID: "B" });
    expect(within(screen.getByRole("main")).getByRole("status")).toBeEmptyDOMElement();
  });

  it("an old route's completion cannot unlock or complete a new route's logout", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse(account()));
    const view = mount(); await ready();
    const a = deferred<Response>();
    fetcher.mockReturnValueOnce(a.promise); logout();
    fetcher.mockResolvedValueOnce(jsonResponse(account("B")));
    view.rerender(tree({ status: "authenticated", userID: "B" }));
    await ready("B");
    const b = deferred<Response>();
    fetcher.mockReturnValueOnce(b.promise); logout();
    await act(async () => { a.resolve(empty()); });
    expect(store.get()).toMatchObject({ status: "authenticated", userID: "B" });
    expect(screen.getByRole("button", { name: "Выходим…" })).toBeDisabled();
    await act(async () => { b.resolve(empty("B")); });
    expect(store.get().status).toBe("anonymous");
    expect(requests().filter((r) => r.startsWith("DELETE"))).toHaveLength(2);
  });
});
