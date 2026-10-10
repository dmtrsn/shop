import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { BrowserSessionStore, RouteSession } from "@/lib/api/browser-session";
import { deferred, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { orderResponse } from "@/test/order-fixtures";
import { PrivateProviders } from "@/test/private-providers";

import { useBrowserSessionStore } from "./browser-session-provider";
import { OrdersView } from "./orders-view";
import { OrderView } from "./order-view";

const fetcher = vi.fn<typeof fetch>();
let store: BrowserSessionStore;
function SessionProbe() { store = useBrowserSessionStore(); return null; }
const initialSession: RouteSession = { status: "authenticated", userID: "A" };
function Tree({ route = initialSession, detail = false }: { route?: RouteSession; detail?: boolean }) {
  return <PrivateProviders><SessionProbe />
    {detail ? <OrderView initialSession={route} orderID="ord-2026-10484" />
      : <OrdersView initialSession={route} />}
  </PrivateProviders>;
}
function requests() { return fetcher.mock.calls.map(([path, init]) => `${init?.method} ${path}`); }
function respond(body: unknown, status = 200, owner: string | null = "A") {
  fetcher.mockResolvedValueOnce(jsonResponse(body, status, owner));
}
beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("order history", () => {
  it.each(["anonymous", "unavailable"] as const)("%s renders a gate with zero requests", (status) => {
    render(<Tree route={{ status }} />);
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(screen.getByRole("heading", { name: status === "anonymous" ? "Войдите, чтобы увидеть заказы" : "История заказов временно недоступна" })).toBeInTheDocument();
    expect(screen.queryByText("Заказов пока нет")).toBeNull();
    if (status === "anonymous") expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login?returnTo=%2F");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("loads once in StrictMode and renders server order, units, money, status, date, address and named links", async () => {
    respond(sessionResponse());
    respond({ items: [orderResponse({ items: null, itemCount: 7 }), orderResponse({ id: "old", number: "#1", items: [], itemCount: 0, deliveryAddress: "" })] });
    render(<StrictMode><Tree /></StrictMode>);
    expect(screen.getByLabelText("Загружаем историю заказов")).toHaveAttribute("aria-busy", "true");
    const list = await screen.findByRole("list", { name: "История заказов" });
    expect(within(list).getAllByRole("article")).toHaveLength(2);
    expect(within(list).getAllByRole("heading").map((h) => h.textContent)).toEqual(["Заказ #10484", "Заказ #1"]);
    expect(screen.getByText("7 единиц")).toBeInTheDocument();
    expect(screen.getByText("0 единиц")).toBeInTheDocument();
    expect(screen.getAllByText("Собираем заказ")).toHaveLength(2);
    expect(screen.getAllByText(/1 268,00/)).toHaveLength(2);
    expect(screen.getByText("Москва, Тестовая, 1")).toBeInTheDocument();
    expect(list.querySelector("time")).toHaveAttribute("datetime", "2026-10-03T12:00:00Z");
    expect(list.querySelector("time")?.textContent).toBe(new Intl.DateTimeFormat("ru-RU", { dateStyle: "long" }).format(new Date("2026-10-03T12:00:00Z")));
    expect(screen.getByRole("link", { name: "Подробнее о заказе #10484" })).toHaveAttribute("href", "/account/orders/ord-2026-10484");
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(screen.getAllByRole("link", { name: "Повторить заказ" }).map((link) => link.getAttribute("href"))).toEqual(["/account/orders/ord-2026-10484/repeat", "/account/orders/old/repeat"]);
    expect(list.querySelector("a button, a a")).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders"]);
    expect(fetcher.mock.calls[1][1]?.headers).toMatchObject({ "X-Expected-Authenticated-User-ID": "A" });
  });

  it("renders empty history without inventing orders", async () => {
    respond(sessionResponse()); respond({ items: [] });
    render(<Tree />);
    expect(await screen.findByRole("heading", { name: "Заказов пока нет" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders"]);
  });

  it.each(["network", "500", "malformed"])("%s is unavailable, and deliberate retry reloads", async (failure) => {
    respond(sessionResponse());
    if (failure === "network") fetcher.mockRejectedValueOnce(new TypeError("offline"));
    else respond(failure === "500" ? { error: { code: "internal_error" } } : { items: [{}] }, failure === "500" ? 500 : 200);
    render(<Tree />);
    await screen.findByRole("heading", { name: "История заказов временно недоступна" });
    expect(screen.queryByText("Заказов пока нет")).toBeNull();
    respond(sessionResponse()); respond({ items: [] });
    fireEvent.click(screen.getByRole("button", { name: "Повторить загрузку" }));
    await screen.findByText("Заказов пока нет");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders", "GET /api/auth/session", "GET /api/orders"]);
  });

  it("401 invalidates the shared session", async () => {
    respond(sessionResponse()); respond({ error: { code: "unauthorized" } }, 401, null);
    render(<Tree />);
    await screen.findByRole("heading", { name: "Войдите, чтобы увидеть заказы" });
    expect(store.get().status).toBe("anonymous");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it.each(["foreign", "missing", "authenticated_user_changed"])("%s owner recovery is bounded and never renders the foreign data", async (failure) => {
    fetcher.mockImplementation(async (path) => String(path) === "/api/auth/session" ? jsonResponse(sessionResponse())
      : failure === "authenticated_user_changed" ? jsonResponse({ error: { code: failure } }, 409, "A")
        : jsonResponse({ items: [orderResponse({ number: "PRIVATE-B" })] }, 200, failure === "missing" ? null : "B"));
    render(<Tree />);
    await screen.findByRole("heading", { name: "История заказов временно недоступна" });
    expect(screen.queryByText(/PRIVATE-B/)).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders", "GET /api/auth/session", "GET /api/orders"]);
  });

  it.each(["anonymous", "unavailable", "B"] as const)("hides loaded A history immediately during A → %s", async (destination) => {
    respond(sessionResponse()); respond({ items: [orderResponse({ number: "PRIVATE-A" })] });
    render(<Tree />);
    await screen.findByText("Заказ PRIVATE-A");
    if (destination === "B") {
      const pendingSession = deferred<Response>();
      fetcher.mockReturnValueOnce(pendingSession.promise);
      let refresh!: Promise<void>;
      act(() => { refresh = store.refresh(); });
      expect(screen.queryByText(/PRIVATE-A/)).toBeNull();
      respond({ items: [orderResponse({ number: "PRIVATE-B" })] }, 200, "B");
      await act(async () => { pendingSession.resolve(jsonResponse(sessionResponse("B"))); await refresh; });
      await screen.findByText("Заказ PRIVATE-B");
      expect(fetcher.mock.calls.at(-1)?.[1]?.headers).toMatchObject({ "X-Expected-Authenticated-User-ID": "B" });
    } else act(() => { store.invalidate(destination); });
    expect(screen.queryByText(/PRIVATE-A/)).toBeNull();
  });

  it.each(["anonymous", "unavailable", "B", "A"])("ignores late A history after transition to %s, even when abort is ignored", async (destination) => {
    respond(sessionResponse());
    const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise);
    render(<Tree />);
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    if (destination === "anonymous" || destination === "unavailable") act(() => { store.invalidate(destination); });
    else {
      respond(sessionResponse(destination)); respond({ items: [] }, 200, destination);
      await act(async () => { await store.refresh(); });
      await screen.findByText("Заказов пока нет");
    }
    await act(async () => { pending.resolve(jsonResponse({ items: [orderResponse({ number: "STALE-A" })] })); });
    expect(screen.queryByText(/STALE-A/)).toBeNull();
    expect(fetcher.mock.calls[1][1]?.signal?.aborted).toBe(true);
  });

  it("history → detail uses the existing detail view and backlink without a placed-order banner", async () => {
    respond(sessionResponse()); respond({ items: [orderResponse()] });
    const { rerender } = render(<Tree />);
    await screen.findByRole("link", { name: "Подробнее о заказе #10484" });
    respond(sessionResponse()); respond(orderResponse());
    rerender(<Tree detail />);
    await screen.findByRole("heading", { level: 1, name: "Заказ #10484" });
    expect(screen.getByRole("link", { name: "Все заказы" })).toHaveAttribute("href", "/account/orders");
    expect(screen.queryByText("Заказ #10484 оформлен")).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders", "GET /api/auth/session", "GET /api/orders/ord-2026-10484"]);
  });
});
