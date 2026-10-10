import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const { replace, navigation } = vi.hoisted(() => ({ replace: vi.fn(), navigation: { path: "/account/orders/ord-1/repeat" } }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace }), usePathname: () => navigation.path }));
import type { BrowserSessionStore, RouteSession } from "@/lib/api/browser-session";
import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { deferred, favoriteProduct, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { repeatLine, repeatPreview, repeatResult } from "@/test/repeat-fixtures";
import { PrivateProviders } from "@/test/private-providers";
import { repeatBody, repeatDrafts } from "@/lib/repeat-review";
import { useBrowserSessionStore } from "./browser-session-provider";
import { useCart } from "./cart-provider";
import { CartView } from "./cart-view";
import { RepeatView } from "./repeat-view";

const fetcher = vi.fn<typeof fetch>();
let store: BrowserSessionStore;
let cart: ReturnType<typeof useCart>;
function Probe() { store = useBrowserSessionStore(); cart = useCart(); return null; }
const route: RouteSession = { status: "authenticated", userID: "A" };
function Tree({ initial = route, page = "repeat" }: { initial?: RouteSession; page?: "repeat" | "cart" | "none" }) {
  return <PrivateProviders><Probe />{page === "repeat" ? <RepeatView orderID="ord-1" initialSession={initial} />
    : page === "cart" ? <CartView initialSession={initial} /> : null}</PrivateProviders>;
}
function respond(body: unknown, status = 200, owner: string | null = "A") { fetcher.mockResolvedValueOnce(jsonResponse(body, status, owner)); }
function error(code: string, status = 409, owner: string | null = "A") { respond({ error: { code } }, status, owner); }
function posts() { return fetcher.mock.calls.filter(([path]) => String(path).endsWith("/commit")); }
function previews() { return fetcher.mock.calls.filter(([path]) => String(path).endsWith("/preview")); }
function submit() { return screen.getByRole("button", { name: "Добавить выбранное в корзину" }); }
function quantity(position = 1) { return screen.getByRole("textbox", { name: new RegExp(`Добавить единиц:.*позиция ${position}\\)`) }); }
async function ready(preview = repeatPreview(), strict = false) {
  respond(sessionResponse()); respond(preview);
  const view = render(strict ? <StrictMode><Tree /></StrictMode> : <Tree />);
  await screen.findByRole("list", { name: "Позиции для повтора" });
  return view;
}
beforeEach(() => { fetcher.mockReset(); replace.mockReset(); navigation.path = "/account/orders/ord-1/repeat"; vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("repeat review", () => {
  it.each(["anonymous", "unavailable"] as const)("%s uses only the server gate", (status) => {
    render(<Tree initial={{ status }} />);
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
  });
  it("loads one preview in StrictMode without initializing Cart", async () => {
    await ready(repeatPreview(), true);
    expect(fetcher.mock.calls.map(([path]) => path)).toEqual(["/api/auth/session", "/api/v2/orders/ord-1/repeat/preview"]);
    expect(cart.cart).toBeNull();
    expect(screen.getByText("1 + 2 = 3 в корзине")).toBeInTheDocument();
    expect(screen.getByText(/Цена изменилась/)).toBeInTheDocument();
  });
  it("shows loading and then missing order", async () => {
    const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise);
    render(<Tree />); expect(screen.getByText("Загружаем условия повтора…")).toBeInTheDocument();
    error("not_found", 404);
    await act(async () => { pending.resolve(jsonResponse(sessionResponse())); });
    await screen.findByRole("heading", { name: "Заказ не найден" });
  });
  it("keeps duplicate source rows, informational replacement and unavailable rows without fake current prices", async () => {
    await ready(repeatPreview(repeatLine(1), repeatLine(5), repeatLine(9, "gone", { status: "discontinued", product: undefined, replacement: favoriteProduct("replacement") }), repeatLine(12, "p02", { status: "unavailable", proposedQuantity: 0, suggestedQuantity: 0 })));
    expect(screen.getAllByRole("listitem")).toHaveLength(4);
    expect(quantity(1)).not.toBe(quantity(5));
    expect(screen.getByRole("link", { name: "Шоколад replacement" })).toHaveAttribute("href", "/products/chocolate-replacement");
    expect(screen.getByText(/Замена не включена/)).toBeInTheDocument();
    expect(screen.getByText(/Текущего предложения/)).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /gone/ })).toBeDisabled();
    expect(screen.getByText(/Сервер проверит их суммарное/)).toBeInTheDocument();
  });
  it("all unavailable and all excluded never send a commit", async () => {
    await ready(repeatPreview(repeatLine(1, "p01", { status: "unavailable", suggestedQuantity: 0, proposedQuantity: 0 })));
    expect(submit()).toBeDisabled(); fireEvent.click(submit());
    expect(screen.getByText(/нет товаров, доступных/)).toBeInTheDocument(); expect(posts()).toHaveLength(0);
  });
  it("edits locally, rejects zero without exclusion, focuses input and omits excluded source lines", async () => {
    await ready(repeatPreview(repeatLine(1), repeatLine(5, "p02")));
    fireEvent.change(quantity(), { target: { value: "0" } }); fireEvent.click(submit());
    expect(await screen.findByText(/Чтобы не добавлять товар/)).toBeInTheDocument();
    expect(quantity()).toHaveFocus(); expect(posts()).toHaveLength(0);
    fireEvent.change(quantity(), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("checkbox", { name: /позиция 5/ }));
    expect(fetcher).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByRole("checkbox", { name: /позиция 1/ }));
    expect(submit()).toBeDisabled(); expect(screen.getByText(/Включите хотя бы/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("checkbox", { name: /позиция 1/ }));
    respond(repeatResult()); fireEvent.click(submit()); await waitFor(() => expect(replace).toHaveBeenCalledWith("/cart"));
    expect(JSON.parse(posts()[0][1]?.body as string).items.map((row: { sourcePosition: number }) => row.sourcePosition)).toEqual([1]);
  });
  it("requires deliberately including proposed-only quantity", async () => {
    await ready(repeatPreview(repeatLine(1, "p01", { suggestedQuantity: 0, proposedQuantity: 4 })));
    expect(submit()).toBeDisabled(); expect(quantity()).toHaveValue("4"); expect(quantity()).toBeDisabled();
    fireEvent.click(screen.getByRole("checkbox")); expect(submit()).toBeEnabled();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

describe("commit and recovery", () => {
  it.each([false, true])("installs authoritative Cart, uses receipt handoff once, replay=%s", async (replayed) => {
    const view = await ready();
    const response = jsonResponse(repeatResult()); if (replayed) response.headers.set("Idempotent-Replayed", "true");
    fetcher.mockResolvedValueOnce(response); fireEvent.click(submit());
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/cart"));
    expect(cart.cart).toEqual(repeatResult().cart);
    navigation.path = "/cart"; view.rerender(<Tree page="cart" />);
    expect(await screen.findByText("Добавлено из заказа #10482: 1 позиция, 2 единицы")).toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledTimes(3);
    view.rerender(<Tree page="none" />);
    respond(sessionResponse()); respond(cartResponse(cartItem("p01", 3)));
    view.rerender(<Tree page="cart" />);
    await screen.findByRole("searchbox", { name: "Найти в корзине" });
    expect(screen.queryByText(/Добавлено из заказа/)).toBeNull();
    expect(fetcher.mock.calls.at(-1)?.[0]).toBe("/api/cart");
  });
  it("double/triple submit and Enter take one synchronous global write lock", async () => {
    await ready(); const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise);
    const form = submit().closest("form")!;
    act(() => { fireEvent.submit(form); fireEvent.submit(form); fireEvent.submit(form); });
    expect(posts()).toHaveLength(1);
    const body = repeatBody(repeatPreview(), repeatDrafts(repeatPreview()));
    await act(async () => {
      expect(await cart.mutate({ kind: "add", productID: "p02", quantity: 1 })).toBe(false);
      expect(await cart.placeOrder({ userID: "A", key: "checkout-same-lock", body: { deliveryAddress: "x", expectedItems: [], expectedTotalMinor: 0 } }, "check")).toEqual({ kind: "blocked" });
      expect(await cart.commitRepeat({ userID: "A", orderID: "ord-1", key: "another-repeat-key", body })).toEqual({ kind: "blocked" });
      pending.resolve(jsonResponse(repeatResult()));
    });
    await waitFor(() => expect(replace).toHaveBeenCalledOnce());
  });
  it.each(["network", "timeout", "500", "malformed", "unexpected"])("%s is unknown; explicit repeated checks keep exact body and key without Cart-as-proof", async (failure) => {
    await ready();
    if (failure === "network" || failure === "timeout") fetcher.mockRejectedValueOnce(new DOMException(failure));
    else if (failure === "500") error("internal_error", 500, null);
    else respond({}, failure === "unexpected" ? 202 : 200);
    fireEvent.click(submit());
    const check = await screen.findByRole("button", { name: "Проверить добавление" });
    await waitFor(() => expect(check).toHaveFocus());
    expect(replace).not.toHaveBeenCalled(); expect(cart.cart).toBeNull();
    fetcher.mockRejectedValueOnce(new Error("offline")); fireEvent.click(check);
    await waitFor(() => expect(screen.getByRole("button", { name: "Проверить добавление" })).toBeEnabled());
    respond(repeatResult()); fireEvent.click(screen.getByRole("button", { name: "Проверить добавление" }));
    await waitFor(() => expect(replace).toHaveBeenCalledOnce());
    expect(posts()).toHaveLength(3);
    expect(new Set(posts().map(([, init]) => (init?.headers as Record<string, string>)["Idempotency-Key"])).size).toBe(1);
    expect(new Set(posts().map(([, init]) => init?.body)).size).toBe(1);
    expect(fetcher).toHaveBeenCalledTimes(5);
  });
  it.each(["repeat_changed", "out_of_stock"])("%s refreshes one preview, preserves safe intent and requires a new explicit key", async (code) => {
    await ready(); fireEvent.change(quantity(), { target: { value: "3" } });
    error(code); respond(repeatPreview(repeatLine(1, "p01", { existingCartQuantity: 2 })));
    fireEvent.click(submit());
    await screen.findByText("2 + 3 = 5 в корзине");
    expect(previews()).toHaveLength(2); expect(posts()).toHaveLength(1);
    expect(quantity()).toHaveValue("3"); expect(screen.getByRole("alert")).toHaveFocus();
    error("validation_error", 422); fireEvent.click(submit()); await screen.findByText(/Добавление не выполнено/);
    const keys = posts().map(([, init]) => (init?.headers as Record<string, string>)["Idempotency-Key"]);
    expect(keys[0]).not.toBe(keys[1]); expect(previews()).toHaveLength(2);
  });
  it("422 focuses the source-position input and ends the attempt while preserving review", async () => {
    await ready(); respond({ error: { code: "validation_error", fields: [{ field: "items[0].quantity", message: "Исправьте итоговое количество" }] } }, 422);
    fireEvent.click(submit()); await waitFor(() => expect(quantity()).toHaveFocus());
    expect(within(quantity().closest("li")!).getByRole("alert")).toHaveTextContent("Исправьте итоговое количество");
    expect(quantity()).toHaveValue("2"); expect(previews()).toHaveLength(1);
  });
  it("csrf clears key, refreshes session and preview with no automatic commit", async () => {
    await ready(); error("csrf_failed", 403, null); respond(sessionResponse("A", "new-csrf")); respond(repeatPreview());
    fireEvent.click(submit()); await waitFor(() => expect(previews()).toHaveLength(2));
    await waitFor(() => expect(submit()).toBeEnabled()); expect(posts()).toHaveLength(1);
    respond(repeatResult()); fireEvent.click(submit()); await waitFor(() => expect(replace).toHaveBeenCalledOnce());
    expect(posts()[1][1]?.headers).toMatchObject({ "X-CSRF-Token": "new-csrf" });
    expect((posts()[0][1]?.headers as Record<string, string>)["Idempotency-Key"]).not.toBe((posts()[1][1]?.headers as Record<string, string>)["Idempotency-Key"]);
  });
  it.each(["idempotency_key_reused", "not_found"])("%s stops commit retry", async (code) => {
    await ready(); error(code, code === "not_found" ? 404 : 409); fireEvent.click(submit());
    if (code === "not_found") await screen.findByText("Заказ не найден");
    else { await screen.findByText(/ключ запроса уже использован/); expect(submit()).toBeDisabled(); }
    expect(posts()).toHaveLength(1); expect(previews()).toHaveLength(1);
  });
  it("401 invalidates and discards the attempt", async () => {
    await ready(); error("unauthorized", 401, null); fireEvent.click(submit());
    await screen.findByText("Войдите, чтобы повторить заказ"); expect(store.get().status).toBe("anonymous");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login?returnTo=%2Faccount%2Forders%2Ford-1%2Frepeat");
    expect(screen.queryByText("Исторический p01")).toBeNull(); expect(fetcher).toHaveBeenCalledTimes(3);
  });
  it.each(["foreign", "authenticated_user_changed"])("%s recovers owner and never replays old attempt", async (kind) => {
    await ready(); if (kind === "foreign") respond(repeatResult(), 200, "B"); else error(kind);
    respond(sessionResponse("B")); respond(repeatPreview(repeatLine(1, "B-product")), 200, "B"); fireEvent.click(submit());
    await screen.findByText("Исторический B-product");
    expect(screen.queryByText("Исторический p01")).toBeNull(); expect(posts()).toHaveLength(1); expect(replace).not.toHaveBeenCalled();
  });
  it.each(["B", "anonymous", "unavailable"] as const)("A → %s hides review and ignores late successful commit", async (next) => {
    await ready(); const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise); fireEvent.click(submit());
    if (next === "B") {
      respond(sessionResponse("B")); respond(repeatPreview(repeatLine(1, "B-product")), 200, "B");
      await act(async () => { await store.refresh(); });
      await screen.findByText("Исторический B-product");
    } else act(() => store.invalidate(next));
    expect(screen.queryByText("Исторический p01")).toBeNull();
    await act(async () => pending.resolve(jsonResponse(repeatResult())));
    expect(replace).not.toHaveBeenCalled(); expect(cart.cart).toBeNull(); expect(posts()).toHaveLength(1);
  });
  it("same-user generation change after send becomes unknown and checks only the frozen operation", async () => {
    await ready(); const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise); fireEvent.click(submit());
    respond(sessionResponse()); await act(async () => { await store.refresh(); });
    await act(async () => pending.resolve(jsonResponse(repeatResult())));
    const check = await screen.findByRole("button", { name: "Проверить добавление" });
    expect(previews()).toHaveLength(1); expect(cart.cart).toBeNull();
    respond(repeatResult()); fireEvent.click(check); await waitFor(() => expect(replace).toHaveBeenCalledOnce());
    expect(posts()[0][1]?.body).toBe(posts()[1][1]?.body);
    expect(fetcher.mock.calls.some(([path]) => path === "/api/cart")).toBe(false);
  });
});

describe("preview and handoff ownership", () => {
  it.each(["malformed", "500", "network"])("preview %s is safely retryable without any commit", async (failure) => {
    respond(sessionResponse());
    if (failure === "network") fetcher.mockRejectedValueOnce(new Error("offline"));
    else respond({}, failure === "500" ? 500 : 200);
    render(<Tree />); await screen.findByText("Не удалось загрузить условия повтора");
    respond(sessionResponse()); respond(repeatPreview()); fireEvent.click(screen.getByRole("button", { name: "Повторить загрузку" }));
    await screen.findByRole("list", { name: "Позиции для повтора" }); expect(posts()).toHaveLength(0);
  });
  it("preview csrf recovery is bounded", async () => {
    fetcher.mockImplementation(async (path) => String(path) === "/api/auth/session" ? jsonResponse(sessionResponse()) : jsonResponse({ error: { code: "csrf_failed" } }, 403, null));
    render(<Tree />); await screen.findByText("Не удалось загрузить условия повтора");
    expect(previews()).toHaveLength(2); expect(fetcher).toHaveBeenCalledTimes(4);
  });
  it("preview foreign-owner recovery is bounded and never renders the body", async () => {
    fetcher.mockImplementation(async (path) => String(path) === "/api/auth/session" ? jsonResponse(sessionResponse()) : jsonResponse(repeatPreview(repeatLine(1, "PRIVATE-B")), 200, "B"));
    render(<Tree />); await screen.findByText("Повтор заказа временно недоступен");
    expect(screen.queryByText(/PRIVATE-B/)).toBeNull(); expect(previews()).toHaveLength(2);
  });
  it("late A preview cannot appear under B", async () => {
    respond(sessionResponse()); const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise);
    render(<Tree />); await waitFor(() => expect(previews()).toHaveLength(1));
    respond(sessionResponse("B")); respond(repeatPreview(repeatLine(1, "B-product")), 200, "B");
    await act(async () => { await store.refresh(); }); await screen.findByText("Исторический B-product");
    await act(async () => pending.resolve(jsonResponse(repeatPreview(repeatLine(1, "PRIVATE-A")))));
    expect(screen.queryByText(/PRIVATE-A/)).toBeNull();
  });
  it.each(["B", "anonymous", "unavailable", "generation", "unrelated"])("success handoff is discarded for %s", async (change) => {
    const view = await ready(); respond(repeatResult()); fireEvent.click(submit()); await waitFor(() => expect(replace).toHaveBeenCalledOnce());
    if (change === "generation" || change === "B") {
      respond(sessionResponse(change === "B" ? "B" : "A")); await act(async () => { await store.refresh(); });
    } else if (change === "anonymous" || change === "unavailable") act(() => store.invalidate(change));
    else { navigation.path = "/categories"; view.rerender(<Tree page="none" />); }
    respond(sessionResponse()); respond(cartResponse(cartItem("p01", 3)));
    navigation.path = "/cart"; view.rerender(<Tree page="cart" />);
    await screen.findByRole("searchbox", { name: "Найти в корзине" });
    expect(screen.queryByText(/Добавлено из заказа/)).toBeNull();
    expect(fetcher.mock.calls.at(-1)?.[0]).toBe("/api/cart");
  });
  it("a warm Cart does not reread during Repeat preview or an unknown same-user generation transition", async () => {
    respond(sessionResponse()); respond(cartResponse(cartItem("p01")));
    const view = render(<Tree page="cart" />); await screen.findByRole("searchbox", { name: "Найти в корзине" });
    respond(sessionResponse()); respond(repeatPreview()); view.rerender(<Tree />);
    await screen.findByRole("list", { name: "Позиции для повтора" });
    const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise); fireEvent.click(submit());
    respond(sessionResponse()); await act(async () => { await store.refresh(); });
    await act(async () => pending.resolve(jsonResponse(repeatResult())));
    await screen.findByRole("button", { name: "Проверить добавление" });
    expect(fetcher.mock.calls.filter(([path]) => path === "/api/cart")).toHaveLength(1);
  });
  it("Repeat cannot overlap a Cart write or Checkout already in progress", async () => {
    respond(sessionResponse()); respond(cartResponse(cartItem("p01")));
    render(<Tree page="cart" />); await screen.findByRole("searchbox", { name: "Найти в корзине" });
    const preview = repeatPreview();
    const attempt = { userID: "A", orderID: "ord-1", key: "repeat-shared-lock", body: repeatBody(preview, repeatDrafts(preview)) };
    const pending = deferred<Response>(); fetcher.mockReturnValueOnce(pending.promise);
    let write!: Promise<boolean>;
    act(() => { write = cart.mutate({ kind: "quantity", productID: "p01", quantity: 2 }); });
    await act(async () => { expect(await cart.commitRepeat(attempt)).toEqual({ kind: "blocked" }); pending.resolve(jsonResponse(cartResponse(cartItem("p01", 2)))); await write; });
    const checkout = deferred<Response>(); fetcher.mockReturnValueOnce(checkout.promise);
    let place!: ReturnType<typeof cart.placeOrder>;
    act(() => { place = cart.placeOrder({ userID: "A", key: "checkout-shared-lock", body: { deliveryAddress: "x", expectedItems: [], expectedTotalMinor: 0 } }, "submit"); });
    await act(async () => { expect(await cart.commitRepeat(attempt)).toEqual({ kind: "blocked" }); checkout.reject(new Error("offline")); await place; });
    expect(posts()).toHaveLength(0);
  });
});


it("a fresh same-user server Cart gate retains normal route synchronization", async () => {
  respond(sessionResponse()); respond(cartResponse(cartItem("p01", 1)));
  const view = render(<Tree page="cart" />);
  await screen.findByRole("searchbox", { name: "Найти в корзине" });
  respond(sessionResponse()); respond(cartResponse(cartItem("p01", 6)));
  view.rerender(<Tree page="cart" initial={{ status: "authenticated", userID: "A" }} />);
  await waitFor(() => expect(cart.cart?.items[0].quantity).toBe(6));
  expect(fetcher.mock.calls.filter(([path]) => path === "/api/cart")).toHaveLength(2);
});
