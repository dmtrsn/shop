import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace, push: vi.fn() }), usePathname: () => "/checkout" }));

import type { Cart } from "@/lib/api/cart";
import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { deferred, favoriteProduct, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { orderResponse } from "@/test/order-fixtures";
import { PrivateProviders } from "@/test/private-providers";

import { useCart, type PlaceOrderResult } from "./cart-provider";
import { CartView } from "./cart-view";
import { CheckoutView } from "./checkout-view";
import { OrderView } from "./order-view";

const fetcher = vi.fn<typeof fetch>();
function respond(body: unknown, status = 200, owner: string | null = "A") {
  fetcher.mockResolvedValueOnce(jsonResponse(body, status, owner));
}
function requests() { return fetcher.mock.calls.map(([path, init]) => `${init?.method} ${path}`); }
function posts() {
  return fetcher.mock.calls.filter(([, init]) => init?.method === "POST").map(([, init]) => ({
    headers: init?.headers as Record<string, string>, body: JSON.parse(init?.body as string),
  }));
}
function placed(body = orderResponse(), replayed = false) {
  const response = jsonResponse(body, 201, "A");
  if (replayed) response.headers.set("Idempotent-Replayed", "true");
  return response;
}
function visibilityReturn() {
  const visibility = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
  act(() => { document.dispatchEvent(new Event("visibilitychange")); });
  visibility.mockReturnValue("visible");
  act(() => { document.dispatchEvent(new Event("visibilitychange")); });
}

// p01 is selected (2 units); p02 stays unselected in the Cart.
function baseCart(): Cart { return cartResponse(cartItem("p01", 2), cartItem("p02", 1, false)); }
const reviewBody = (cart: Cart) => ({
  expectedItems: cart.items.filter((line) => line.selected).map((line) => ({ productId: line.product.id, quantity: line.quantity, unitPriceMinor: line.product.priceMinor })),
  expectedTotalMinor: cart.summary.totalMinor,
});

function mountCheckout(status: "authenticated" | "anonymous" | "unavailable" = "authenticated") {
  return render(
    <PrivateProviders loginHref="https://shop.test/#/login">
      <CheckoutView initialSession={status === "authenticated" ? { status, userID: "A" } : { status }} />
    </PrivateProviders>,
  );
}
async function openCheckout(cart: Cart = baseCart()) {
  respond(sessionResponse(), 200, null);
  respond(cart);
  const view = mountCheckout();
  await screen.findByRole("heading", { name: "Товары в заказе" });
  return view;
}
const address = () => screen.getByLabelText("Адрес доставки") as HTMLTextAreaElement;
const commentField = () => screen.getByLabelText(/Комментарий к заказу/) as HTMLTextAreaElement;
const submitButton = () => screen.getByRole("button", { name: /Подтвердить заказ|Оформляем заказ/ });
function fill(value = "Москва, Тестовая, 1", comment = "") {
  fireEvent.change(address(), { target: { value } });
  if (comment) fireEvent.change(commentField(), { target: { value: comment } });
}

let uuid = 0;
beforeEach(() => {
  fetcher.mockReset(); replace.mockReset(); vi.stubGlobal("fetch", fetcher);
  uuid = 0;
  vi.spyOn(globalThis.crypto, "randomUUID").mockImplementation(() => `00000000-0000-4000-8000-00000000000${++uuid}` as `${string}-${string}-${string}-${string}-${string}`);
});
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("/checkout states", () => {
  it.each(["anonymous", "unavailable"] as const)("%s is a gate, never an empty review, and fetches nothing", (status) => {
    mountCheckout(status);
    expect(screen.getByRole("heading", { level: 1, name: "Оформление заказа" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: status === "anonymous" ? "Войдите, чтобы оформить заказ" : "Оформление временно недоступно" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Адрес доставки")).toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("loading shows a busy skeleton until a fresh Cart arrives", () => {
    fetcher.mockReturnValue(new Promise(() => {}));
    mountCheckout();
    expect(screen.getByLabelText("Загружаем заказ")).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByRole("button", { name: "Подтвердить заказ" })).toBeNull();
  });

  it("zero selected lines offer the Cart and no form", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse(cartItem("p01", 1, false)));
    mountCheckout();
    expect(await screen.findByRole("heading", { name: "Нет товаров, выбранных для заказа" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Вернуться в корзину" })).toHaveAttribute("href", "/cart");
    expect(screen.queryByLabelText("Адрес доставки")).toBeNull();
  });

  it("a selected line with an issue is listed and blocks submission without any request", async () => {
    const short = { ...cartItem("p03", 3), issue: "Доступно только 2", available: 2 };
    await openCheckout(cartResponse(cartItem("p01", 2), short));
    expect(screen.getByText("Оформить заказ пока нельзя.")).toBeInTheDocument();
    expect(screen.getByText("Шоколад p03: Доступно только 2")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Исправить в корзине" })).toHaveAttribute("href", "/cart");
    fill();
    expect(submitButton()).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(submitButton());
    expect(posts()).toHaveLength(0);
  });

  it("reviews selected lines only with Go values verbatim and collects only address and comment", async () => {
    const cart = baseCart();
    Object.assign(cart.summary, { subtotalMinor: 100000, discountMinor: 2500, deliveryMinor: 29000, totalMinor: 12345 });
    const { container } = await openCheckout(cart);
    const review = screen.getByRole("region", { name: "Товары в заказе" });
    expect(within(review).getByText("Шоколад p01")).toBeInTheDocument();
    expect(within(review).queryByText("Шоколад p02")).toBeNull();
    expect(within(review).getByText("Арт. SKU-p01 · Мастерская")).toBeInTheDocument();
    const summary = screen.getByRole("complementary", { name: "Итоги заказа" });
    for (const amount of [/1 000,00/, /25,00/, /290,00/, /123,45/]) expect(within(summary).getByText(amount)).toBeInTheDocument();
    expect(within(summary).getByText("Не выбрано и останется в корзине: 1 позиция.")).toBeInTheDocument();
    expect(address()).toHaveAttribute("autocomplete", "street-address");
    expect(address()).toHaveValue("");
    expect(address()).not.toHaveAttribute("maxlength");
    expect(commentField()).not.toHaveAttribute("maxlength");
    expect(container.querySelectorAll("input, textarea, select")).toHaveLength(2);
    expect(screen.queryByLabelText(/телефон|имя|e-?mail|оплат/i)).toBeNull();
    expect(container.querySelector("a button, button a, a a, button button")).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart"]);
  });

  it("a blank address is caught locally, focused and never sent", async () => {
    await openCheckout();
    fill("   ");
    fireEvent.click(submitButton());
    expect(await screen.findByText("Укажите адрес доставки")).toBeInTheDocument();
    expect(address()).toHaveAttribute("aria-invalid", "true");
    expect(address().getAttribute("aria-describedby")).toContain("checkout-address-error");
    await waitFor(() => expect(address()).toHaveFocus());
    expect(posts()).toHaveLength(0);
  });
});

describe("/checkout submission", () => {
  it("sends one keyed POST with the exact frozen review; double click and Enter cannot add another", async () => {
    const cart = baseCart();
    await openCheckout(cart);
    fill(" Москва, Тестовая, 1 ", "  Домофон 12 ");
    const response = deferred<Response>();
    fetcher.mockReturnValueOnce(response.promise);
    fireEvent.click(submitButton());
    fireEvent.click(submitButton());
    fireEvent.submit(address().form!);
    expect(await screen.findByRole("button", { name: "Оформляем заказ…" })).toHaveAttribute("aria-disabled", "true");
    expect(address()).toHaveAttribute("readonly");
    expect(posts()).toHaveLength(1);
    expect(posts()[0].body).toEqual({ deliveryAddress: " Москва, Тестовая, 1 ", comment: "  Домофон 12 ", ...reviewBody(cart) });
    expect(posts()[0].headers).toMatchObject({
      "Idempotency-Key": "00000000-0000-4000-8000-000000000001", "X-CSRF-Token": "csrf-A", "X-Expected-Authenticated-User-ID": "A",
    });
    await act(async () => response.resolve(placed()));
    await waitFor(() => expect(replace).toHaveBeenCalledExactlyOnceWith("/account/orders/ord-2026-10484"));
    expect(screen.getByText("Заказ оформлен. Открываем подтверждение…")).toBeInTheDocument();
    // No Cart read is used as confirmation and no line is fabricated locally.
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders"]);
  });

  it("omits an empty comment and accepts a replayed 201 as success", async () => {
    await openCheckout();
    fill();
    fetcher.mockResolvedValueOnce(placed(orderResponse(), true));
    fireEvent.click(submitButton());
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/account/orders/ord-2026-10484"));
    expect(posts()[0].body).not.toHaveProperty("comment");
  });

  it("checkout_changed rereads, keeps the form, labels changes in text, focuses the alert and rotates the key", async () => {
    await openCheckout();
    fill("Адрес", "Комментарий");
    respond({ error: { code: "checkout_changed", message: "x" } }, 409);
    const changed = cartResponse({ ...cartItem("p01", 3), product: favoriteProduct("p01", { priceMinor: 50000 }), lineTotalMinor: 150000 }, cartItem("p04", 1), cartItem("p02", 1, false));
    respond(changed);
    fireEvent.click(submitButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Заказ не оформлен: корзина изменилась");
    await waitFor(() => expect(alert).toHaveFocus());
    expect(screen.getByText("Изменение: Количество изменилось: было 2, стало 3")).toBeInTheDocument();
    expect(screen.getByText(/Изменение: Цена изменилась: было 489,00\s₽, стало 500,00\s₽/)).toBeInTheDocument();
    expect(screen.getByText("Изменение: Новая позиция в заказе")).toBeInTheDocument();
    expect(screen.getByText(/Изменение: Итого изменилось/)).toBeInTheDocument();
    expect(address()).toHaveValue("Адрес");
    expect(commentField()).toHaveValue("Комментарий");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders", "GET /api/cart"]);
    await waitFor(() => expect(submitButton()).toHaveAttribute("aria-disabled", "false"));
    fetcher.mockResolvedValueOnce(placed());
    fireEvent.click(submitButton());
    await waitFor(() => expect(posts()).toHaveLength(2));
    expect(posts()[1].headers["Idempotency-Key"]).toBe("00000000-0000-4000-8000-000000000002");
    expect(posts()[1].body).toMatchObject(reviewBody(changed));
  });

  it("out_of_stock rereads and blocks on the refreshed issue; removed lines are listed", async () => {
    await openCheckout(cartResponse(cartItem("p01", 2), cartItem("p05", 1)));
    fill();
    respond({ error: { code: "out_of_stock" } }, 409);
    respond(cartResponse({ ...cartItem("p01", 2), issue: "Доступно только 1", available: 1 }));
    fireEvent.click(submitButton());
    expect(await screen.findByRole("alert")).toHaveTextContent("товара недостаточно");
    expect(await screen.findByText("Шоколад p01: Доступно только 1")).toBeInTheDocument();
    expect(screen.getByText("Шоколад p05 · 1 единица")).toBeInTheDocument();
    expect(submitButton()).toHaveAttribute("aria-disabled", "true");
    expect(posts()).toHaveLength(1);
  });

  it("conflict rereads to the empty-selection state without replay", async () => {
    await openCheckout();
    fill();
    respond({ error: { code: "conflict" } }, 409);
    respond(cartResponse(cartItem("p02", 1, false)));
    fireEvent.click(submitButton());
    expect(await screen.findByRole("heading", { name: "Нет товаров, выбранных для заказа" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Заказ не оформлен");
    expect(posts()).toHaveLength(1);
  });

  it("422 maps field errors, focuses the first invalid field, keeps values and does not reread", async () => {
    await openCheckout();
    fill("Адрес", "Комментарий");
    respond({ error: { code: "validation_error", message: "x", fields: [{ field: "deliveryAddress", message: "Укажите адрес доставки" }] } }, 422);
    fireEvent.click(submitButton());
    expect(await screen.findByText("Укажите адрес доставки")).toBeInTheDocument();
    await waitFor(() => expect(address()).toHaveFocus());
    expect(address()).toHaveAttribute("aria-invalid", "true");
    expect(commentField()).toHaveValue("Комментарий");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders"]);
    fireEvent.change(address(), { target: { value: "Новый адрес" } });
    expect(address()).not.toHaveAttribute("aria-invalid");
    fetcher.mockResolvedValueOnce(placed());
    fireEvent.click(submitButton());
    await waitFor(() => expect(posts()).toHaveLength(2));
    expect(posts()[1].headers["Idempotency-Key"]).toBe("00000000-0000-4000-8000-000000000002");
  });

  it("csrf_failed checks the session and rereads; it never replays and the next submit is a new attempt", async () => {
    await openCheckout();
    fill("Адрес");
    respond({ error: { code: "csrf_failed" } }, 403, null);
    respond(sessionResponse("A", "new-csrf"), 200, null);
    respond(baseCart());
    fireEvent.click(submitButton());
    expect(await screen.findByRole("alert")).toHaveTextContent("Мы проверили вход");
    await waitFor(() => expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders", "GET /api/auth/session", "GET /api/cart"]));
    expect(address()).toHaveValue("Адрес");
    await waitFor(() => expect(submitButton()).toHaveAttribute("aria-disabled", "false"));
    fetcher.mockResolvedValueOnce(placed());
    fireEvent.click(submitButton());
    await waitFor(() => expect(posts()).toHaveLength(2));
    expect(posts()[1].headers).toMatchObject({ "X-CSRF-Token": "new-csrf", "Idempotency-Key": "00000000-0000-4000-8000-000000000002" });
  });

  it("401 signs out and hides the review without replay", async () => {
    await openCheckout();
    fill();
    respond({ error: { code: "unauthorized" } }, 401, null);
    fireEvent.click(submitButton());
    expect(await screen.findByRole("heading", { name: "Войдите, чтобы оформить заказ" })).toBeInTheDocument();
    expect(screen.queryByText("Шоколад p01")).toBeNull();
    expect(posts()).toHaveLength(1);
  });

  it("idempotency_key_reused is definitive: an alert, a Cart link and no retry", async () => {
    await openCheckout();
    fill();
    respond({ error: { code: "idempotency_key_reused" } }, 409);
    fireEvent.click(submitButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Не удалось подтвердить заказ");
    expect(within(alert).getByRole("link", { name: "Вернуться в корзину" })).toHaveAttribute("href", "/cart");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders"]);
  });

  it.each([
    ["a foreign owner", () => jsonResponse(orderResponse(), 201, "B")],
    ["authenticated_user_changed", () => jsonResponse({ error: { code: "authenticated_user_changed" } }, 409, "B")],
  ])("%s is discarded unread, recovers the session once and drops the attempt", async (_name, make) => {
    await openCheckout();
    fill("Адрес A");
    fetcher.mockResolvedValueOnce(make());
    respond(sessionResponse("B"), 200, null);
    respond(cartResponse(cartItem("p09", 1)), 200, "B");
    fireEvent.click(submitButton());
    await waitFor(() => expect(screen.getByText("Шоколад p09")).toBeInTheDocument());
    expect(address()).toHaveValue("");
    expect(replace).not.toHaveBeenCalled();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders", "GET /api/auth/session", "GET /api/cart"]);
  });
});

describe("/checkout ambiguous outcome", () => {
  it.each([
    ["network failure", () => Promise.reject(new TypeError("offline"))],
    ["5xx", () => Promise.resolve(jsonResponse({ error: { code: "internal_error" } }, 500, "A"))],
    ["malformed 201", () => Promise.resolve(jsonResponse({ id: "partial" }, 201, "A"))],
  ])("%s: no verdict, no Cart read, no resend; «Проверить заказ» reuses the same key and body", async (_name, make) => {
    await openCheckout();
    fill("Адрес", "Комментарий");
    fetcher.mockImplementationOnce(make);
    fireEvent.click(submitButton());
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Не удалось узнать результат оформления");
    expect(alert).toHaveTextContent("Заказ мог быть создан");
    const check = screen.getByRole("button", { name: "Проверить заказ" });
    await waitFor(() => expect(check).toHaveFocus());
    expect(address()).toHaveAttribute("readonly");
    expect(commentField()).toHaveAttribute("readonly");
    fireEvent.change(address(), { target: { value: "Изменённый" } });
    expect(address()).toHaveValue("Адрес");
    expect(screen.queryByRole("button", { name: "Подтвердить заказ" })).toBeNull();
    expect(screen.getByRole("link", { name: "Вернуться в корзину" })).toHaveAttribute("href", "/cart");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/orders"]);

    // A second unknown keeps the same key; a later replay is success.
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    fireEvent.click(check);
    expect(await screen.findByText("Результат всё ещё неизвестен")).toBeInTheDocument();
    fetcher.mockResolvedValueOnce(placed(orderResponse(), true));
    fireEvent.click(screen.getByRole("button", { name: "Проверить заказ" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/account/orders/ord-2026-10484"));
    const sent = posts();
    expect(sent).toHaveLength(3);
    expect(new Set(sent.map((post) => post.headers["Idempotency-Key"]))).toEqual(new Set(["00000000-0000-4000-8000-000000000001"]));
    expect(sent[1].body).toEqual(sent[0].body);
    expect(sent[2].body).toEqual(sent[0].body);
    expect(requests().filter((request) => request === "GET /api/cart")).toHaveLength(1);
  });

  it("a check that ends definitively reopens a new attempt with a new key", async () => {
    await openCheckout();
    fill();
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    fireEvent.click(submitButton());
    const check = await screen.findByRole("button", { name: "Проверить заказ" });
    respond({ error: { code: "checkout_changed" } }, 409);
    respond(baseCart());
    fireEvent.click(check);
    expect(await screen.findByText("Заказ не оформлен: корзина изменилась")).toBeInTheDocument();
    await waitFor(() => expect(submitButton()).toHaveAttribute("aria-disabled", "false"));
    fetcher.mockResolvedValueOnce(placed());
    fireEvent.click(submitButton());
    await waitFor(() => expect(posts()).toHaveLength(3));
    expect(posts().map((post) => post.headers["Idempotency-Key"].slice(-1))).toEqual(["1", "1", "2"]);
  });

  it("a same-user session revalidation during the POST makes a 201 unknown, never published; the check replays", async () => {
    await openCheckout();
    fill();
    const response = deferred<Response>();
    fetcher.mockReturnValueOnce(response.promise);
    respond(sessionResponse("A", "csrf-A2"), 200, null);
    respond(baseCart());
    fireEvent.click(submitButton());
    visibilityReturn();
    await waitFor(() => expect(requests()).toContain("GET /api/auth/session"));
    await act(async () => response.resolve(placed()));
    expect(await screen.findByText("Не удалось узнать результат оформления")).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    fetcher.mockResolvedValueOnce(placed(orderResponse(), true));
    fireEvent.click(screen.getByRole("button", { name: "Проверить заказ" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/account/orders/ord-2026-10484"));
    expect(posts()[1].headers).toMatchObject({ "Idempotency-Key": posts()[0].headers["Idempotency-Key"], "X-CSRF-Token": "csrf-A2" });
  });

  it.each([
    ["B", () => sessionResponse("B")],
    ["anonymous", () => ({ authenticated: false, demoLogin: false })],
    ["unavailable", () => "not json"],
  ])("A → %s during the POST discards the result and A's form; nothing is replayed", async (next, make) => {
    await openCheckout();
    fill("Адрес A");
    const response = deferred<Response>();
    fetcher.mockReturnValueOnce(response.promise);
    const sessionBody = make();
    if (typeof sessionBody === "string") fetcher.mockResolvedValueOnce(new Response(sessionBody, { status: 200 }));
    else respond(sessionBody, 200, null);
    if (next === "B") respond(cartResponse(cartItem("p09", 1)), 200, "B");
    fireEvent.click(submitButton());
    visibilityReturn();
    await waitFor(() => expect(screen.queryByText("Шоколад p01")).toBeNull());
    await act(async () => response.resolve(placed()));
    if (next === "B") expect(await screen.findByText("Шоколад p09")).toBeInTheDocument();
    else expect(await screen.findByRole("heading", { name: next === "anonymous" ? "Войдите, чтобы оформить заказ" : "Оформление временно недоступно" })).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    expect(posts()).toHaveLength(1);
    if (next === "B") expect(address()).toHaveValue("");
  });
});

describe("order confirmation", () => {
  function Flow() {
    const [route, setRoute] = useState<string | null>(null);
    replace.mockImplementation((href: string) => setRoute(href));
    return route
      ? <OrderView initialSession={{ status: "authenticated", userID: "A" }} orderID={decodeURIComponent(route.split("/").pop()!)} />
      : <CheckoutView initialSession={{ status: "authenticated", userID: "A" }} />;
  }

  it("the success handoff shows the banner once with the session/order/Cart pattern", async () => {
    respond(sessionResponse(), 200, null);
    respond(baseCart());
    render(<PrivateProviders loginHref={null}><Flow /></PrivateProviders>);
    await screen.findByRole("heading", { name: "Товары в заказе" });
    fill();
    fetcher.mockResolvedValueOnce(placed());
    fetcher.mockImplementation(async (path) => String(path) === "/api/auth/session"
      ? jsonResponse(sessionResponse(), 200, null)
      : String(path) === "/api/cart" ? jsonResponse(cartResponse(cartItem("p02", 1, false))) : jsonResponse(orderResponse()));
    fireEvent.click(submitButton());
    const banner = await screen.findByText("Заказ #10484 оформлен");
    await waitFor(() => expect(banner).toHaveFocus());
    expect(banner).toHaveAttribute("role", "status");
    expect(screen.getByRole("heading", { level: 1, name: "Заказ #10484" })).toBeInTheDocument();
    await waitFor(() => expect(requests().slice(3).sort()).toEqual(["GET /api/auth/session", "GET /api/cart", "GET /api/orders/ord-2026-10484"].sort()));
    expect(requests().filter((request) => request.includes("/api/products"))).toEqual([]);
  });

  function mountOrder(status: "authenticated" | "anonymous" | "unavailable" = "authenticated", id = "ord-2026-10484") {
    return render(<PrivateProviders loginHref={null}>
      <OrderView initialSession={status === "authenticated" ? { status, userID: "A" } : { status }} orderID={id} />
    </PrivateProviders>);
  }

  it("a direct load shows the snapshot without a banner, Cart or product reads", async () => {
    respond(sessionResponse(), 200, null);
    respond(orderResponse({ comment: "Домофон 12", items: [{ ...orderResponse().items[0], manufacturer: "" }] }));
    mountOrder();
    expect(await screen.findByRole("heading", { level: 1, name: "Заказ #10484" })).toBeInTheDocument();
    expect(screen.queryByText(/оформлен$/)).toBeNull();
    expect(screen.getByRole("link", { name: "Все заказы" })).toHaveAttribute("href", "/account/orders");
    const facts = screen.getByText("Статус").closest("dl")!;
    expect(within(facts).getByText("Собираем заказ")).toBeInTheDocument();
    expect(within(facts).getByText("Москва, Тестовая, 1")).toBeInTheDocument();
    expect(within(facts).getByText("Домофон 12")).toBeInTheDocument();
    expect(within(facts).queryByText("Доставлен")).toBeNull();
    expect(screen.getByRole("link", { name: "Шоколад p01" })).toHaveAttribute("href", "/products/p01");
    const total = screen.getByRole("complementary", { name: "Сумма заказа" });
    expect(within(total).getByText("Итого с доставкой")).toBeInTheDocument();
    expect(within(total).getByText(/1 268,00/)).toBeInTheDocument();
    expect(within(total).getByText("2 единицы")).toBeInTheDocument();
    expect(screen.queryByText(/Доставка|Скидка|без скидки/)).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders/ord-2026-10484"]);
    const headers = fetcher.mock.calls[1][1]?.headers as Record<string, string>;
    expect(headers["X-Expected-Authenticated-User-ID"]).toBe("A");
  });

  it("404 is a not-found state for missing and foreign orders alike", async () => {
    respond(sessionResponse(), 200, null);
    respond({ error: { code: "not_found" } }, 404);
    mountOrder("authenticated", "#10482");
    expect(await screen.findByRole("heading", { name: "Заказ не найден" })).toBeInTheDocument();
    expect(requests()[1]).toBe("GET /api/orders/%2310482");
  });

  it.each(["anonymous", "unavailable"] as const)("%s gates without requests", (status) => {
    mountOrder(status);
    expect(screen.getByRole("heading", { name: status === "anonymous" ? "Войдите, чтобы открыть заказ" : "Заказ временно недоступен" })).toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("a foreign-owned order is never rendered; recovery is bounded", async () => {
    fetcher.mockImplementation(async (path) => String(path) === "/api/auth/session"
      ? jsonResponse(sessionResponse(), 200, null) : jsonResponse(orderResponse(), 200, "B"));
    mountOrder();
    expect(await screen.findByRole("heading", { name: "Заказ временно недоступен" })).toBeInTheDocument();
    expect(screen.queryByText("Шоколад p01")).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/orders/ord-2026-10484", "GET /api/auth/session", "GET /api/orders/ord-2026-10484"]);
  });
});

describe("Cart checkout link", () => {
  function mountCart() {
    return render(<PrivateProviders loginHref={null}><CartView initialSession={{ status: "authenticated", userID: "A" }} /></PrivateProviders>);
  }
  async function openCart(cart: Cart) {
    respond(sessionResponse(), 200, null);
    respond(cart);
    mountCart();
    await screen.findByRole("heading", { level: 2, name: /Итоги выбранного/ });
  }

  it("links to /checkout for a settled Cart with a clean selection and never writes", async () => {
    await openCart(baseCart());
    const link = screen.getByRole("link", { name: "Перейти к оформлению" });
    expect(link).toHaveAttribute("href", "/checkout");
    expect(link).not.toHaveAttribute("aria-disabled");
    fireEvent.click(link);
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart"]);
  });

  it("offers guidance instead of a link without a selection or with a selected issue", async () => {
    await openCart(cartResponse(cartItem("p01", 1, false)));
    expect(screen.queryByRole("link", { name: "Перейти к оформлению" })).toBeNull();
    expect(screen.getByText("Выберите товары, чтобы оформить заказ.")).toBeInTheDocument();
  });

  it("a selected issue blocks the link", async () => {
    await openCart(cartResponse({ ...cartItem("p01", 3), issue: "Доступно только 2", available: 2 }));
    expect(screen.queryByRole("link", { name: "Перейти к оформлению" })).toBeNull();
    expect(screen.getByText(/снимите выбор с позиций с проблемой наличия/)).toBeInTheDocument();
  });

  it("is aria-disabled while a Cart write is pending", async () => {
    await openCart(baseCart());
    const write = deferred<Response>();
    fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(screen.getByRole("checkbox", { name: "Выбрать для заказа: Шоколад p02" }));
    await waitFor(() => expect(screen.getByRole("link", { name: /Перейти к оформлению/ })).toHaveAttribute("aria-disabled", "true"));
    await act(async () => write.resolve(jsonResponse(baseCart())));
    await waitFor(() => expect(screen.getByRole("link", { name: "Перейти к оформлению" })).not.toHaveAttribute("aria-disabled"));
  });
});

describe("placeOrder shares the global Cart write lock", () => {
  const attempt = { userID: "A", key: "lock-key-000000000001", body: { deliveryAddress: "A", ...reviewBody(baseCart()) } };
  const results: PlaceOrderResult[] = [];
  function Probe() {
    const cart = useCart();
    return <>
      <p data-testid="status">{cart.status}</p>
      <button onClick={() => void cart.ensure()}>ensure</button>
      <button onClick={() => void cart.mutate({ kind: "selected", productID: "p01", selected: false })}>write</button>
      <button onClick={() => void cart.placeOrder(attempt, "submit").then((result) => results.push(result))}>order</button>
      <button onClick={() => void cart.placeOrder({ ...attempt, userID: "B" }, "submit").then((result) => results.push(result))}>order-as-B</button>
    </>;
  }

  it("an order cannot start during a Cart write, a Cart write is dropped during an order, and another user's attempt is refused", async () => {
    results.length = 0;
    respond(sessionResponse(), 200, null);
    respond(baseCart());
    render(<PrivateProviders loginHref={null}><Probe /></PrivateProviders>);
    fireEvent.click(screen.getByText("ensure"));
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("ready"));
    const write = deferred<Response>();
    fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(screen.getByText("write"));
    fireEvent.click(screen.getByText("order"));
    await waitFor(() => expect(results).toEqual([{ kind: "blocked" }]));
    await act(async () => write.resolve(jsonResponse(baseCart())));
    fireEvent.click(screen.getByText("order-as-B"));
    await waitFor(() => expect(results).toHaveLength(2));
    expect(results[1]).toEqual({ kind: "blocked" });
    const order = deferred<Response>();
    fetcher.mockReturnValueOnce(order.promise);
    fireEvent.click(screen.getByText("order"));
    fireEvent.click(screen.getByText("order"));
    fireEvent.click(screen.getByText("write"));
    await act(async () => order.resolve(placed()));
    await waitFor(() => expect(results.map((result) => result.kind)).toEqual(["blocked", "blocked", "blocked", "placed"]));
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "PATCH /api/cart/items/p01", "POST /api/orders"]);
  });
});
