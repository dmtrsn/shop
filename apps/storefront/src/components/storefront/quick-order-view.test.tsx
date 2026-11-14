import { useSyncExternalStore, type ComponentProps } from "react";
import type Link from "next/link";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Cart } from "@/lib/api/cart";
import type { Product } from "@/lib/api/product-dto";
import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { deferred, favoriteProduct, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { PrivateProviders } from "@/test/private-providers";

vi.mock("next/navigation", () => ({
  usePathname: () => "/quick-order",
  useSearchParams: () => new URLSearchParams(useSyncExternalStore(
    (listener) => { window.addEventListener("popstate", listener); return () => window.removeEventListener("popstate", listener); },
    () => window.location.search,
  )),
}));
vi.mock("next/link", () => ({ default: ({ href, children, prefetch, ...props }: ComponentProps<typeof Link>) => <a {...props} data-prefetch={String(prefetch)} href={String(href)}>{children}</a> }));

import { QuickOrderView } from "./quick-order-view";

const manufacturers = [
  { id: "b", slug: "beta", name: "ИП Бета", country: "Россия", productCount: 1 },
  { id: "a", slug: "alpha", name: "ООО «Альфа»", country: "Россия", productCount: 4 },
];
const product = (id = "p01", overrides: Partial<Product> = {}) => favoriteProduct(id, {
  name: "Сладкая шалость с персиком 2,5 кг", manufacturer: manufacturers[1], unit: "кор.", minimumQuantity: 2, quantityStep: 3, stock: 11, ...overrides,
});
let products: Product[];
let saved: Cart;
let requests: string[];
let writes: { path: string; body: Record<string, unknown> }[];
let catalog: ((url: URL, init?: RequestInit) => Promise<Response>) | undefined;
let mutation: (() => Promise<Response>) | undefined;
let refreshed: Product;
const fetcher = vi.fn<typeof fetch>(async (input, init) => {
  const url = new URL(String(input), "http://localhost");
  const method = init?.method ?? "GET";
  requests.push(`${method} ${url.pathname}`);
  if (url.pathname === "/api/auth/session") return jsonResponse(sessionResponse(), 200, null);
  if (url.pathname === "/api/cart" && method === "GET") return jsonResponse(saved);
  if (url.pathname === "/api/catalog") {
    if (catalog) return catalog(url, init);
    return page(products);
  }
  if (url.pathname.startsWith("/api/products/")) return jsonResponse(refreshed, 200, null);
  if (url.pathname.startsWith("/api/cart/items")) {
    const body = JSON.parse(String(init?.body));
    writes.push({ path: `${method} ${url.pathname}`, body });
    if (mutation) return mutation();
    const old = saved.items.find((line) => line.product.id === "p01");
    saved = cartResponse({ ...cartItem("p01", body.quantity, old?.selected ?? true), product: products[0] });
    return jsonResponse(saved, method === "POST" ? 201 : 200);
  }
  throw new Error(`Unexpected request ${method} ${url.pathname}`);
});
function page(items: Product[]) { return jsonResponse({ items, page: 1, limit: 100, total: items.length, totalPages: Math.ceil(items.length / 100) }, 200, null); }
function mount() { return render(<PrivateProviders><QuickOrderView manufacturers={manufacturers} /></PrivateProviders>); }
function choose(name = manufacturers[1].name) { fireEvent.click(screen.getByRole("button", { name })); }
function search() { return screen.getByRole("searchbox", { name: "Поиск товара" }) as HTMLInputElement; }
function input(name = products[0].name) { return screen.getByRole("textbox", { name: `Количество: ${name}` }); }
function add(name = products[0].name) { return screen.getByRole("button", { name: `Добавить: ${name}` }); }
function typeQuantity(value: string) { fireEvent.change(input(), { target: { value } }); }
async function ready() {
  mount(); choose();
  await screen.findByRole("link", { name: products[0].name });
  await waitFor(() => expect(add()).toHaveAttribute("aria-disabled", "false"));
}
function navigate(slug: string) { act(() => { window.history.pushState(null, "", `?manufacturer=${slug}`); }); }

beforeEach(() => {
  products = [product()]; saved = cartResponse(); requests = []; writes = []; catalog = undefined; mutation = undefined; refreshed = product();
  fetcher.mockClear(); vi.stubGlobal("fetch", fetcher);
  window.history.replaceState(null, "", "/quick-order");
  const push = window.history.pushState.bind(window.history);
  vi.spyOn(window.history, "pushState").mockImplementation((...args) => { push(...args); window.dispatchEvent(new PopStateEvent("popstate")); });
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  Object.defineProperty(HTMLDialogElement.prototype, "showModal", { configurable: true, value: function (this: HTMLDialogElement) { this.open = true; this.querySelector("input")?.focus(); } });
  Object.defineProperty(HTMLDialogElement.prototype, "close", { configurable: true, value: function (this: HTMLDialogElement) { this.open = false; this.dispatchEvent(new Event("close")); } });
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("Fast Order manufacturers and route", () => {
  it("shows full official names in Slice 1 order, filters locally and writes only the canonical slug", async () => {
    mount();
    const directory = screen.getByRole("complementary", { name: "Производители" });
    expect(within(directory).getAllByRole("button").map((button) => button.textContent)).toEqual(["ООО «Альфа»", "ИП Бета"]);
    fireEvent.change(within(directory).getByRole("searchbox"), { target: { value: "бета ип" } });
    expect(within(directory).queryByRole("button", { name: "ООО «Альфа»" })).toBeNull();
    choose("ИП Бета");
    expect(window.location.search).toBe("?manufacturer=beta");
    await screen.findByRole("heading", { name: "ИП Бета" });
    expect(requests.filter((request) => request.includes("manufacturers"))).toHaveLength(0);
  });

  it("retains a direct URL selection and shows an honest unknown-slug state", async () => {
    window.history.replaceState(null, "", "?manufacturer=alpha"); mount();
    await screen.findByRole("link", { name: products[0].name });
    navigate("missing");
    expect(screen.getByText(/Этот производитель не найден/)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: products[0].name })).toBeNull();
  });

  it("clears query, draft and product scroll when switching and returning through history", async () => {
    await ready(); typeQuantity("5");
    fireEvent.change(search(), { target: { value: "персик" } });
    within(screen.getByRole("region", { name: /Товары:/ })).getByRole("list").parentElement!.scrollTop = 200;
    choose("ИП Бета"); expect(search()).toHaveValue("");
    await screen.findByRole("link", { name: products[0].name });
    navigate("alpha");
    await screen.findByRole("link", { name: products[0].name });
    expect(search()).toHaveValue(""); expect(input()).toHaveValue("");
    expect(within(screen.getByRole("region", { name: /Товары:/ })).getByRole("list").parentElement!.scrollTop).toBe(0);
  });

  it("keeps both search inputs outside their respective lists", async () => {
    await ready();
    const directory = screen.getByRole("complementary", { name: "Производители" });
    expect(within(directory).getByRole("list")).not.toContainElement(within(directory).getByRole("searchbox"));
    const region = screen.getByRole("region", { name: /Товары:/ });
    const results = within(region).getByRole("list").parentElement!;
    expect(results).not.toContainElement(search());
    expect(region).toContainElement(search());
  });

  it("mobile chooser is named, searchable, closes after selection and returns focus", async () => {
    mount(); const trigger = screen.getByRole("button", { name: "Выбрать производителя" });
    fireEvent.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "Производители" });
    fireEvent.change(within(dialog).getByRole("searchbox"), { target: { value: "альфа" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "ООО «Альфа»" }));
    expect(window.location.search).toBe("?manufacturer=alpha");
    expect(dialog).not.toHaveAttribute("open"); expect(trigger).toHaveFocus();
    fireEvent.click(trigger);
    expect(within(screen.getByRole("dialog")).getByRole("searchbox")).toHaveValue("");
    fireEvent.click(within(dialog).getByRole("button", { name: "Закрыть" }));
    expect(trigger).toHaveFocus();
    await screen.findByRole("link", { name: products[0].name });
  });
});

describe("Fast Order complete loads and stale generations", () => {
  it("A → B → A aborts both old loads and ignores their late success/error", async () => {
    const loads = [deferred<Response>(), deferred<Response>(), deferred<Response>()];
    const signals: AbortSignal[] = [];
    catalog = async (_url, init) => { signals.push(init!.signal!); return loads[signals.length - 1].promise; };
    mount(); choose(); await waitFor(() => expect(signals).toHaveLength(1));
    choose("ИП Бета"); await waitFor(() => expect(signals).toHaveLength(2));
    choose(); await waitFor(() => expect(signals).toHaveLength(3));
    expect(signals[0].aborted).toBe(true); expect(signals[1].aborted).toBe(true);
    expect(screen.queryByRole("textbox")).toBeNull();
    await act(async () => { loads[0].resolve(page([product("old", { name: "Старый A" })])); loads[1].resolve(new Response("{}", { status: 500 })); });
    expect(screen.queryByText("Старый A")).toBeNull(); expect(screen.queryByRole("alert")).toBeNull();
    await act(async () => { loads[2].resolve(page(products)); });
    expect(await screen.findByRole("link", { name: products[0].name })).toBeInTheDocument();
  });

  it("removes loaded products immediately; partial pages never appear as results", async () => {
    await ready();
    const second = deferred<Response>();
    catalog = async (url) => url.searchParams.get("page") === "1"
      ? jsonResponse({ items: [product("partial", { name: "Частичный товар" })], page: 1, limit: 100, total: 101, totalPages: 2 }, 200, null)
      : second.promise;
    choose("ИП Бета");
    expect(screen.queryByRole("link", { name: products[0].name })).toBeNull();
    fireEvent.change(search(), { target: { value: "неизвестно" } });
    expect(screen.queryByText("Ничего не найдено у этого производителя")).toBeNull();
    expect(screen.queryByText("Частичный товар")).toBeNull();
    await act(async () => { second.resolve(new Response("{}", { status: 500 })); });
    expect(await screen.findByRole("alert")).toHaveTextContent("Не удалось загрузить товары");
    catalog = undefined;
    fireEvent.click(screen.getByRole("button", { name: "Повторить загрузку товаров" }));
    expect(await screen.findByText("Ничего не найдено у этого производителя")).toBeInTheDocument();
    fireEvent.change(search(), { target: { value: "" } });
    expect(await screen.findByRole("link", { name: products[0].name })).toBeInTheDocument();
  });

  it("distinguishes a successfully loaded empty manufacturer", async () => {
    products = []; mount(); choose();
    expect(await screen.findByText("У этого производителя пока нет товаров")).toBeInTheDocument();
  });
});

describe("Fast Order products", () => {
  it("keeps alphabetic rows, secondary SKU and PDP links, uses local fragment/SKU search, and displays no images", async () => {
    products.push(product("a", { name: "Абрикос" }), product("z", { name: "Яблоко", image: "/real.jpg" }));
    await ready();
    // The products region scopes away the manufacturer directory's list.
    const region = screen.getByRole("region", { name: /Товары:/ });
    expect(within(region).getAllByRole("link").map((link) => link.textContent)).toEqual(["Абрикос", products[0].name, "Яблоко"]);
    expect(region.querySelector("img")).toBeNull();
    expect(within(region).getByText("арт. SKU-p01").className).toContain("secondary");
    expect(screen.getByRole("link", { name: products[0].name })).toHaveAttribute("href", "/products/chocolate-p01");
    const count = fetcher.mock.calls.length;
    fireEvent.change(search(), { target: { value: "персик шалость" } });
    expect(within(region).getAllByRole("link")).toHaveLength(1);
    fireEvent.change(search(), { target: { value: "sku-p01" } });
    expect(within(region).getAllByRole("link")).toHaveLength(1);
    expect(fetcher).toHaveBeenCalledTimes(count);
  });

  it("keeps zero stock and below-minimum offers visible with disabled purchase controls", async () => {
    products = [product("zero", { name: "Нулевой", stock: 0 }), product("low", { name: "Мало", stock: 1 })];
    mount(); choose(); await screen.findByText("Нет в наличии");
    expect(screen.getByText(/меньше минимального количества 2 кор/)).toBeInTheDocument();
    for (const p of products) {
      expect(input(p.name)).toBeDisabled(); expect(add(p.name)).toBeDisabled();
      expect(screen.getByRole("button", { name: `Увеличить количество: ${p.name}` })).toBeDisabled();
      expect(screen.getByRole("link", { name: p.name })).toHaveAttribute("href", `/products/${p.slug}`);
    }
  });
});

describe("Fast Order quantity and shared cart", () => {
  it("begins blank; plus/minus only edits offset-step drafts without writes", async () => {
    await ready(); expect(input()).toHaveValue("");
    const minus = screen.getByRole("button", { name: /Уменьшить количество:/ });
    const plus = screen.getByRole("button", { name: /Увеличить количество:/ });
    expect(minus).toBeDisabled(); fireEvent.click(plus); expect(input()).toHaveValue("2");
    fireEvent.click(plus); expect(input()).toHaveValue("5");
    fireEvent.click(minus); expect(input()).toHaveValue("2");
    typeQuantity("1."); expect(input()).toHaveValue("1."); expect(plus).toBeDisabled();
    expect(writes).toHaveLength(0);
  });

  it.each([["", "Укажите количество."], ["1.5", "Введите целое число."], ["-1", "Минимальное количество: 2."], ["1", "Минимальное количество: 2."], ["6", "Допустимые количества: от 2 с шагом 3."], ["14", "Доступно только 11."]])("explains invalid %s and Enter cannot submit it", async (draft, message) => {
    await ready(); typeQuantity(draft);
    await userEvent.click(input()); await userEvent.keyboard("{Enter}");
    expect(screen.getAllByText(message, { exact: false }).length).toBeGreaterThan(0);
    expect(input()).toHaveAttribute("aria-invalid", "true");
    const ids = input().getAttribute("aria-describedby")!.split(" ");
    expect(ids.every((id) => document.getElementById(id))).toBe(true);
    expect(writes).toHaveLength(0); expect(input()).toHaveValue(draft);
  });

  it("explains minimum and step failures together", async () => {
    await ready(); typeQuantity("1");
    expect(input()).toHaveAccessibleDescription(/Минимальное количество: 2.*от 2 с шагом 3/);
  });

  it("offers explicit suggestion without rounding the typed draft; submits the explicit quantity", async () => {
    await ready(); typeQuantity("6"); expect(input()).toHaveValue("6");
    const suggestion = screen.getByRole("button", { name: "Добавить 8" });
    await userEvent.click(suggestion);
    await screen.findByText("✓ В корзине: 8 кор.");
    expect(writes[0].body).toEqual({ productId: "p01", quantity: 8 });
  });

  it("explains stock and step failures together and offers an optional valid quantity", async () => {
    products = [product("p01", { stock: 50 })];
    await ready(); typeQuantity("99");
    expect(input()).toHaveAccessibleDescription(/Доступно только 50.*от 2 с шагом 3/);
    expect(screen.getByRole("button", { name: "Добавить 50" })).toBeInTheDocument();
    expect(input()).toHaveValue("99"); expect(writes).toHaveLength(0);
    typeQuantity("2"); expect(input()).toHaveAttribute("aria-invalid", "false");
  });

  it("Add and valid Enter use the same new-line mutation and authoritative highlight", async () => {
    await ready(); typeQuantity("5");
    const pending = deferred<Response>(); mutation = () => pending.promise;
    await userEvent.click(add());
    expect(screen.queryByText(/✓ В корзине/)).toBeNull();
    await act(async () => { pending.resolve(jsonResponse(cartResponse({ ...cartItem("p01", 5), product: products[0] }), 201)); });
    const status = await screen.findByText("✓ В корзине: 5 кор.");
    expect(status.closest("li")?.className).toContain("confirmed");
    expect(writes[0]).toEqual({ path: "POST /api/cart/items", body: { productId: "p01", quantity: 5 } });
    expect(input()).toHaveValue("");
    mutation = undefined; typeQuantity("8"); await userEvent.click(input()); await userEvent.keyboard("{Enter}");
    await screen.findByText("✓ В корзине: 8 кор.");
    expect(writes[1]).toEqual({ path: "PATCH /api/cart/items/p01", body: { quantity: 8 } });
  });

  it("existing cart uses absolute quantity-only PATCH and preserves unselected state", async () => {
    saved = cartResponse({ ...cartItem("p01", 5, false), product: products[0] });
    mount(); choose(); await screen.findByText("✓ В корзине: 5 кор.");
    expect(input()).toHaveValue(""); typeQuantity("8");
    await userEvent.click(screen.getByRole("button", { name: /Обновить количество:/ }));
    await screen.findByText("✓ В корзине: 8 кор.");
    expect(writes).toEqual([{ path: "PATCH /api/cart/items/p01", body: { quantity: 8 } }]);
    expect(saved.items[0].selected).toBe(false); expect(saved.items[0].quantity).toBe(8);
  });

  it("preserves rejected drafts and refreshes constraints for a new explicit suggestion", async () => {
    await ready(); typeQuantity("5");
    refreshed = product("p01", { minimumQuantity: 3, quantityStep: 3, stock: 10 });
    mutation = async () => jsonResponse({ error: { code: "invalid_quantity", detail: "Правила изменились" } }, 422);
    await userEvent.click(add());
    expect(await screen.findByRole("button", { name: "Добавить 6" })).toBeInTheDocument();
    expect(input()).toHaveValue("5"); expect(writes).toHaveLength(1);
    expect(requests.filter((request) => request.includes("/api/products/"))).toEqual(["GET /api/products/p01"]);
    expect(screen.queryByText(/✓ В корзине/)).toBeNull();
  });

  it("lets CartProvider reconcile uncertain writes without a second mutation", async () => {
    await ready(); typeQuantity("5");
    mutation = async () => {
      saved = cartResponse({ ...cartItem("p01", 5), product: products[0] });
      throw new TypeError("connection lost");
    };
    await userEvent.click(add());
    expect(await screen.findByText("✓ В корзине: 5 кор.")).toBeInTheDocument();
    await waitFor(() => expect(requests.filter((request) => request === "GET /api/cart")).toHaveLength(2));
    expect(writes).toHaveLength(1); expect(input()).toHaveValue("5");
  });

  it("shows one global position count and the authoritative payable total", async () => {
    saved = cartResponse(cartItem("other", 19)); saved.summary.totalMinor = 4235000;
    mount(); choose();
    const summary = screen.getByLabelText("Общая корзина");
    await waitFor(() => expect(summary).toHaveTextContent("В корзине: 1 позиция · К оплате: 42 350,00 ₽"));
    expect(within(summary).getAllByRole("link")).toHaveLength(1);
    expect(requests.filter((request) => request === "GET /api/cart")).toHaveLength(1);
  });

  it("loading and unavailable cart states never fabricate zero totals", async () => {
    fetcher.mockImplementationOnce(() => new Promise(() => {}));
    mount(); expect(screen.getByLabelText("Общая корзина")).toHaveTextContent("Загружаем корзину");
    expect(screen.getByLabelText("Общая корзина")).not.toHaveTextContent("0");
  });

  it("unavailable cart offers recovery while public products remain browsable", async () => {
    fetcher.mockImplementationOnce(async () => new Response("{}", { status: 503 }));
    mount(); choose();
    await screen.findByRole("button", { name: "Повторить проверку корзины" });
    expect(screen.getByLabelText("Общая корзина")).toHaveTextContent("Корзина недоступна");
    await screen.findByRole("link", { name: products[0].name });
    expect(add()).toHaveAttribute("aria-disabled", "true");
  });

  it("renders 500 ordinary rows with complete paging and no per-row requests", async () => {
    products = Array.from({ length: 500 }, (_, i) => product(`p${i}`, { name: `Товар ${String(i).padStart(3, "0")}` }));
    catalog = async (url) => {
      const number = Number(url.searchParams.get("page"));
      return jsonResponse({ items: products.slice((number - 1) * 100, number * 100), page: number, limit: 100, total: 500, totalPages: 5 }, 200, null);
    };
    mount(); choose();
    const region = screen.getByRole("region", { name: /Товары:/ });
    await within(region).findByText("Товар 499");
    expect(within(region).getAllByRole("link", { hidden: true })).toHaveLength(500);
    fireEvent.change(search(), { target: { value: "499" } });
    expect(within(region).getAllByRole("link")).toHaveLength(1);
    expect(requests.filter((request) => request === "GET /api/cart")).toHaveLength(1);
    expect(requests.filter((request) => request === "GET /api/catalog")).toHaveLength(5);
    expect(requests.some((request) => request.includes("/api/products/"))).toBe(false);
  }, 15000);
});

describe("Fast Order focus ownership", () => {
  it("successful Add keeps query, focuses search and selects all text", async () => {
    await ready(); fireEvent.change(search(), { target: { value: "шалость" } }); typeQuantity("5");
    await userEvent.click(add());
    await waitFor(() => expect(search()).toHaveFocus());
    expect(search()).toHaveValue("шалость"); expect(search().selectionStart).toBe(0); expect(search().selectionEnd).toBe(7);
  });

  it.each(["focus", "query", "manufacturer", "away-and-back"])("does not steal focus after %s changes while pending", async (change) => {
    await ready(); typeQuantity("5");
    const pending = deferred<Response>(); mutation = () => pending.promise;
    await userEvent.click(add());
    if (change === "manufacturer") choose("ИП Бета");
    if (change === "query") fireEvent.change(search(), { target: { value: "персик" } });
    if (change === "focus" || change === "away-and-back") {
      screen.getByRole("searchbox", { name: "Поиск производителя" }).focus();
      if (change === "away-and-back") input().focus();
    }
    const before = document.activeElement;
    await act(async () => { pending.resolve(jsonResponse(cartResponse({ ...cartItem("p01", 5), product: products[0] }), 201)); });
    if (change !== "manufacturer") expect(document.activeElement).toBe(before);
    else expect(search()).not.toHaveFocus();
  });
});
