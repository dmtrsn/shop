import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Product } from "@/lib/api/product-dto";
import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { deferred, favoriteProduct, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { PrivateProviders } from "@/test/private-providers";

import { CartControl } from "./cart-control";
import { CartQuantityEditor } from "./cart-quantity-editor";
import { CartView } from "./cart-view";
import { CompactProductCard, ProductCard } from "./product-card";

const fetcher = vi.fn<typeof fetch>();
function respond(body: unknown, status = 200, owner: string | null = "A") {
  fetcher.mockResolvedValueOnce(jsonResponse(body, status, owner));
}
function requests() { return fetcher.mock.calls.map(([path, init]) => `${init?.method} ${path}`); }
function sentBody(index: number) { return JSON.parse(fetcher.mock.calls[index][1]?.body as string); }
function page(status: "authenticated" | "anonymous" | "unavailable" = "authenticated") {
  return render(
    <PrivateProviders>
      <CartView initialSession={status === "authenticated" ? { status, userID: "A" } : { status }} />
    </PrivateProviders>,
  );
}
function control(product: Product, detail = false) {
  return render(<PrivateProviders><CartControl detail={detail} product={product} /></PrivateProviders>);
}
async function openCart(cart: ReturnType<typeof cartResponse>) {
  respond(sessionResponse(), 200, null);
  respond(cart);
  const view = page();
  await screen.findByRole("heading", { level: 2, name: /Итоги выбранного/ });
  return view;
}

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("/cart route states", () => {
  it.each(["anonymous", "unavailable"] as const)("%s never looks like an empty Cart and fetches nothing", (status) => {
    page(status);
    expect(screen.getByRole("heading", {
      name: status === "anonymous" ? "Войдите, чтобы открыть корзину" : "Корзина временно недоступна",
    })).toBeInTheDocument();
    expect(screen.queryByText("В корзине пока нет товаров")).toBeNull();
    if (status === "anonymous") expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login?returnTo=%2F");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("authenticated loading shows a skeleton, not an empty state", () => {
    fetcher.mockReturnValue(new Promise(() => {}));
    page();
    expect(screen.getByLabelText("Загружаем корзину")).toHaveAttribute("aria-busy", "true");
    expect(screen.queryByText("В корзине пока нет товаров")).toBeNull();
  });

  it("an empty authenticated Cart offers catalog navigation", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    page();
    expect(await screen.findByText("В корзине пока нет товаров")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart"]);
  });
});

describe("/cart list", () => {
  it("renders Go totals verbatim, exact counts, groups, links and issues without product requests", async () => {
    const other = { id: "other", slug: "other", name: "Артель", country: "Россия" };
    const issue = { ...cartItem("p02", 3), issue: "Доступно только 2", available: 2 };
    const third = { ...cartItem("p03"), product: favoriteProduct("p03", { manufacturer: other }) };
    const body = cartResponse(cartItem(), issue, third);
    Object.assign(body.summary, { subtotalMinor: 100000, discountMinor: 2500, deliveryMinor: 0, totalMinor: 12345 });
    const { container } = await openCart(body);
    const summary = screen.getByRole("complementary", { name: "Итоги выбранного" });
    expect(within(summary).getByText(/123,45/)).toBeInTheDocument();
    expect(within(summary).getByText(/1 000,00/)).toBeInTheDocument();
    expect(within(summary).getByText("Выбрано 3 из 3 позиций · 5 единиц")).toBeInTheDocument();
    expect(within(summary).getByText(/С проблемой наличия: 1 позиция/)).toBeInTheDocument();
    expect(screen.getByText("3 позиции · 5 единиц")).toBeInTheDocument();
    expect(screen.getByText("Проблема наличия: Доступно только 2")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Шоколад p01" })).toHaveAttribute("href", "/products/chocolate-p01");
    expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent))
      .toEqual(expect.arrayContaining([expect.stringContaining("Мастерская"), expect.stringContaining("Артель")]));
    expect(container.querySelector("a button, a input, button a, button button, label button")).toBeNull();
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart"]);
  });

  it("whole-Cart selection ignores search and collapsed groups and sends explicit canonical IDs", async () => {
    const other = { id: "other", slug: "other", name: "Артель", country: "Россия" };
    const third = { ...cartItem("p03"), product: favoriteProduct("p03", { manufacturer: other }) };
    const body = cartResponse(cartItem(), cartItem("p02"), third);
    await openCart(body);
    fireEvent.change(screen.getByRole("searchbox", { name: "Найти в корзине" }), { target: { value: "SKU-p01" } });
    expect(screen.queryByRole("link", { name: "Шоколад p02" })).toBeNull();
    expect(screen.getByText(/Показано 1 из 3 позиций/)).toBeInTheDocument();
    fireEvent.change(screen.getByRole("searchbox", { name: "Найти в корзине" }), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: /^Артель/ }));
    expect(screen.getByRole("button", { name: /^Артель/ })).toHaveAttribute("aria-expanded", "false");
    fireEvent.change(screen.getByRole("searchbox", { name: "Найти в корзине" }), { target: { value: "p01" } });
    // Searching and collapsing are local only.
    expect(fetcher).toHaveBeenCalledTimes(2);

    respond(body);
    fireEvent.click(screen.getByRole("button", { name: "Снять выбор со всей корзины" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
    expect(requests()[2]).toBe("PATCH /api/cart/selection");
    expect(sentBody(2)).toEqual({ productIds: ["p01", "p02", "p03"], selected: false });
    await waitFor(() => expect(screen.getByRole("button", { name: /Выбрать всю корзину/ })).toHaveAttribute("aria-disabled", "false"));

    respond(body);
    fireEvent.click(screen.getByRole("button", { name: "Выбрать всю корзину (3 позиции)" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    expect(sentBody(3)).toEqual({ productIds: ["p01", "p02", "p03"], selected: true });
  });

  it("an item checkbox is non-optimistic and sends a selection-only PATCH", async () => {
    await openCart(cartResponse(cartItem(), cartItem("p02")));
    const checkbox = screen.getByRole("checkbox", { name: "Выбрать для заказа: Шоколад p01" });
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    fireEvent.click(checkbox);
    expect(checkbox).toBeChecked();
    expect(requests()[2]).toBe("PATCH /api/cart/items/p01");
    expect(sentBody(2)).toEqual({ selected: false });
    await act(async () => pending.resolve(jsonResponse(cartResponse(cartItem("p01", 1, false), cartItem("p02")))));
    expect(checkbox).not.toBeChecked();
  });

  it("removal uses only the per-item endpoint, waits for Go and recovers focus", async () => {
    const user = userEvent.setup();
    await openCart(cartResponse(cartItem(), cartItem("p02")));
    const first = screen.getByRole("button", { name: "Удалить из корзины: Шоколад p01" });
    first.focus();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    await user.keyboard("{Enter}");
    expect(screen.getByRole("link", { name: "Шоколад p01" })).toBeInTheDocument();
    expect(first).toHaveAttribute("aria-disabled", "true");
    await act(async () => pending.resolve(jsonResponse(cartResponse(cartItem("p02")))));
    const second = screen.getByRole("button", { name: "Удалить из корзины: Шоколад p02" });
    await waitFor(() => expect(second).toHaveFocus());
    respond(cartResponse());
    await user.keyboard("{Enter}");
    await waitFor(() => expect(screen.getByRole("heading", { level: 1, name: "Корзина" })).toHaveFocus());
    expect(requests().slice(2)).toEqual(["DELETE /api/cart/items/p01", "DELETE /api/cart/items/p02"]);
  });

  it("409 preserves the attempted draft, rereads and never clamps", async () => {
    await openCart(cartResponse(cartItem()));
    const input = screen.getByRole("textbox", { name: "Количество: Шоколад p01" });
    fireEvent.change(input, { target: { value: "4" } });
    respond({ error: { code: "out_of_stock", message: "Недостаточно товара" } }, 409);
    const line = cartItem();
    line.product.stock = 2;
    line.available = 2;
    respond(cartResponse(line));
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(4));
    await waitFor(() => expect(input).toHaveAttribute("aria-invalid", "true"));
    expect(input).toHaveValue("4");
    expect(screen.getByText("Недостаточно товара")).toBeInTheDocument();
    expect(input.getAttribute("aria-describedby")).toContain(screen.getByText("Недостаточно товара").id);
    expect(sentBody(2)).toEqual({ quantity: 4 });
    expect(requests().slice(2)).toEqual(["PATCH /api/cart/items/p01", "GET /api/cart"]);
  });

  it("422 keeps the confirmed Cart and the invalid draft, without rereading", async () => {
    await openCart(cartResponse(cartItem()));
    const input = screen.getByRole("textbox", { name: "Количество: Шоколад p01" });
    fireEvent.change(input, { target: { value: "3" } });
    respond({ error: { code: "validation_error", fields: [{ field: "quantity", message: "Количество не соответствует шагу" }] } }, 422);
    fireEvent.keyDown(input, { key: "Enter" });
    await screen.findByText("Количество не соответствует шагу");
    expect(input).toHaveValue("3");
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText(/Сумма:/).parentElement).toHaveTextContent("489,00");
    expect(requests().slice(2)).toEqual(["PATCH /api/cart/items/p01"]);
  });

  it("confirmed totals change only after Go answers a quantity change", async () => {
    await openCart(cartResponse(cartItem()));
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    fireEvent.click(screen.getByRole("button", { name: "Увеличить количество: Шоколад p01" }));
    expect(sentBody(2)).toEqual({ quantity: 2 });
    expect(screen.getByText(/Сумма:/).parentElement).toHaveTextContent("489,00");
    await act(async () => pending.resolve(jsonResponse(cartResponse(cartItem("p01", 2)))));
    expect(screen.getByText(/Сумма:/).parentElement).toHaveTextContent("978,00");
  });
});

describe("product Cart control", () => {
  it.each([false, true])("first add POSTs an absolute minimum or explicit quantity once (detail=%s)", async (detail) => {
    const product = favoriteProduct("p01", { minimumQuantity: 3, quantityStep: 2 });
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    control(product, detail);
    const button = screen.getByRole("button", { name: `Добавить в корзину: ${product.name}` });
    await waitFor(() => expect(button).toHaveAttribute("aria-disabled", "false"));
    if (detail) {
      fireEvent.click(screen.getByRole("button", { name: `Увеличить количество: ${product.name}` }));
      expect(screen.getByRole("textbox", { name: `Количество: ${product.name}` })).toHaveValue("5");
      expect(fetcher).toHaveBeenCalledTimes(2);
    }
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    fireEvent.click(button);
    fireEvent.click(button);
    expect(fetcher).toHaveBeenCalledTimes(3);
    expect(requests()[2]).toBe("POST /api/cart/items");
    expect(sentBody(2)).toEqual({ productId: "p01", quantity: detail ? 5 : 3 });
    const line = { ...cartItem("p01", detail ? 5 : 3), product };
    await act(async () => pending.resolve(jsonResponse(cartResponse(line), 201)));
    expect(screen.getByRole("textbox", { name: `Количество: ${product.name}` })).toHaveValue(String(line.quantity));
    expect(screen.getByRole("link", { name: detail ? "Перейти в корзину" : "В корзине" })).toHaveAttribute("href", "/cart");
  });

  it("an existing line PATCHes an absolute quantity instead of POSTing a delta", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse(cartItem("p01", 2)));
    control(favoriteProduct("p01"));
    fireEvent.click(await screen.findByRole("button", { name: "Увеличить количество: Шоколад p01" }));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
    expect(requests()[2]).toBe("PATCH /api/cart/items/p01");
    expect(sentBody(2)).toEqual({ quantity: 3 });
  });

  it("detail validates the explicit draft locally before any write", async () => {
    const product = favoriteProduct("p01", { minimumQuantity: 2, quantityStep: 2, stock: 6 });
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    control(product, true);
    const button = screen.getByRole("button", { name: `Добавить в корзину: ${product.name}` });
    await waitFor(() => expect(button).toHaveAttribute("aria-disabled", "false"));
    const input = screen.getByRole("textbox", { name: `Количество: ${product.name}` });
    for (const draft of ["3", "8", "0"]) {
      fireEvent.change(input, { target: { value: draft } });
      fireEvent.click(button);
      expect(input).toHaveAttribute("aria-invalid", "true");
    }
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("an out-of-stock product offers no Add action", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    control(favoriteProduct("p01", { stock: 0 }), true);
    expect(await screen.findByText("Покупка недоступна")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Добавить в корзину/ })).toBeNull();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("anonymous shoppers get sign-in guidance and no guest Cart", async () => {
    respond({ authenticated: false }, 200, null);
    render(<PrivateProviders><CartControl product={favoriteProduct()} /></PrivateProviders>);
    expect(await screen.findByRole("link", { name: "Войдите, чтобы купить: Шоколад p01" })).toHaveAttribute("href", "/login?returnTo=%2F");
    expect(requests()).toEqual(["GET /api/auth/session"]);
  });

  it.each([
    ["comfortable", ProductCard],
    ["compact", CompactProductCard],
  ] as const)("%s ProductCard embeds the shared control beside, not inside, its links", async (_name, Card) => {
    fetcher.mockImplementation(async (path) => String(path) === "/api/auth/session"
      ? jsonResponse(sessionResponse(), 200, null)
      : jsonResponse(String(path) === "/api/cart" ? cartResponse() : { items: [], groups: [], total: 0 }));
    const product = favoriteProduct();
    const { container } = render(<PrivateProviders><Card product={product} /></PrivateProviders>);
    const add = screen.getByRole("button", { name: `Добавить в корзину: ${product.name}` });
    await waitFor(() => expect(add).toHaveAttribute("aria-disabled", "false"));
    expect(add.closest("a")).toBeNull();
    expect(container.querySelector("a button, a input, button a")).toBeNull();
  });
});

describe("quantity editor", () => {
  const product = favoriteProduct("p01", { minimumQuantity: 3, quantityStep: 2, stock: 9 });
  function editor(commit = vi.fn().mockResolvedValue(true), quantity = 5, busy = false) {
    render(<CartQuantityEditor busy={busy} commit={commit} product={product} quantity={quantity} />);
    return { commit, input: screen.getByRole("textbox", { name: `Количество: ${product.name}` }) };
  }

  it.each([
    ["empty", ""], ["zero", "0"], ["below minimum", "1"], ["off step", "4"], ["above stock", "11"],
    ["decimal", "3.5"], ["negative", "-3"], ["unsafe", "9007199254740993"],
  ])("rejects %s without writing and marks the draft invalid", (_name, draft) => {
    const { commit, input } = editor();
    fireEvent.change(input, { target: { value: draft } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(commit).not.toHaveBeenCalled();
    expect(input).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveValue(draft);
    const describedBy = input.getAttribute("aria-describedby")!.split(" ");
    expect(describedBy.map((id) => document.getElementById(id))).not.toContain(null);
  });

  it("typing never writes; Enter or Apply commits; Escape restores the confirmed value", async () => {
    const { commit, input } = editor();
    expect(screen.queryByRole("button", { name: /Применить/ })).toBeNull();
    fireEvent.change(input, { target: { value: "7" } });
    expect(commit).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: `Применить количество: ${product.name}` }));
    await waitFor(() => expect(commit).toHaveBeenCalledExactlyOnceWith(7));
    fireEvent.change(input, { target: { value: "9" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(input).toHaveValue("5");
    fireEvent.change(input, { target: { value: "9" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(commit).toHaveBeenLastCalledWith(9));
    expect(commit).toHaveBeenCalledTimes(2);
  });

  it("+/- commit one absolute step from the confirmed value", async () => {
    const { commit } = editor();
    fireEvent.click(screen.getByRole("button", { name: `Увеличить количество: ${product.name}` }));
    await waitFor(() => expect(commit).toHaveBeenLastCalledWith(7));
    fireEvent.click(screen.getByRole("button", { name: `Уменьшить количество: ${product.name}` }));
    await waitFor(() => expect(commit).toHaveBeenLastCalledWith(3));
  });

  it("minimum disables decrement; zero never deletes and points to Remove", () => {
    const { commit, input } = editor(undefined, 3);
    const decrement = screen.getByRole("button", { name: `Уменьшить количество: ${product.name}` });
    expect(decrement).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(decrement);
    fireEvent.change(input, { target: { value: "0" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(commit).not.toHaveBeenCalled();
    expect(screen.getByText(/используйте «Удалить»/)).toBeInTheDocument();
  });

  it("increment is disabled at the stock boundary", () => {
    const { commit } = editor(undefined, 9);
    const increment = screen.getByRole("button", { name: `Увеличить количество: ${product.name}` });
    expect(increment).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(increment);
    expect(commit).not.toHaveBeenCalled();
  });

  it("an unchanged value never writes", () => {
    const { commit, input } = editor();
    fireEvent.change(input, { target: { value: "5" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(commit).not.toHaveBeenCalled();
  });

  it("a pending write blocks +/-, Enter and Apply instead of queueing", () => {
    const { commit, input } = editor(undefined, 5, true);
    fireEvent.click(screen.getByRole("button", { name: `Увеличить количество: ${product.name}` }));
    fireEvent.change(input, { target: { value: "7" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: `Применить количество: ${product.name}` }));
    expect(commit).not.toHaveBeenCalled();
  });

  it("newer typing survives completion of an earlier commit", async () => {
    const pending = deferred<boolean>();
    const { input } = editor(vi.fn().mockReturnValue(pending.promise), 3);
    fireEvent.change(input, { target: { value: "5" } });
    fireEvent.keyDown(input, { key: "Enter" });
    fireEvent.change(input, { target: { value: "7" } });
    await act(async () => pending.resolve(true));
    expect(input).toHaveValue("7");
  });
});
