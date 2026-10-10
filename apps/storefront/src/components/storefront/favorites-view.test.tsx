import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, favoriteProduct, favoriteResponse, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { cartResponse } from "@/test/cart-fixtures";
import { FavoriteButton } from "./favorite-button";
import { FavoritesProvider, type FavoritesRouteSession } from "@/test/private-providers";
import { FavoritesView } from "./favorites-view";
import { ProductCard, CompactProductCard } from "./product-card";

const p1 = favoriteProduct("one");
const p2 = favoriteProduct("two", { stock: 0 });
const fetcher = vi.fn<typeof fetch>();
function respond(body: unknown) { fetcher.mockResolvedValueOnce(jsonResponse(body)); }
function page(session: FavoritesRouteSession = { status: "authenticated", userID: "A" }) {
  return render(<FavoritesProvider><FavoritesView initialSession={session} /></FavoritesProvider>);
}
beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", (path: RequestInfo | URL, options?: RequestInit) => String(path) === "/api/cart" ? Promise.resolve(jsonResponse(cartResponse())) : fetcher(path, options)); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("favorites route states and list", () => {
  it("anonymous server gate does not load private data and links to the Next sign-in page", () => {
    page({ status: "anonymous" });
    expect(screen.getByRole("heading", { name: "Войдите, чтобы открыть избранное" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login?returnTo=%2F");
    expect(fetcher).not.toHaveBeenCalled();
    expect(screen.queryByText("В избранном пока нет товаров")).not.toBeInTheDocument();
  });
  it("signed-out copy keeps a catalog exit next to the sign-in link", () => {
    page({ status: "anonymous" });
    expect(screen.getByRole("heading", { name: /Войдите/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
  });
  it("session unavailable has an explicit retry and can recover", async () => {
    page({ status: "unavailable" });
    expect(screen.getByRole("heading", { name: "Избранное временно недоступно" })).toBeInTheDocument();
    expect(screen.queryByText(/Войдите, чтобы/)).not.toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
    respond(sessionResponse()); respond(favoriteResponse(p1));
    fireEvent.click(screen.getByRole("button", { name: "Повторить загрузку" }));
    expect(await screen.findByRole("link", { name: p1.name })).toHaveAttribute("href", `/products/${p1.slug}`);
  });
  it("renders the initial skeleton then a distinct authenticated empty state", async () => {
    const session = deferred<Response>(); fetcher.mockReturnValueOnce(session.promise); respond(favoriteResponse());
    page();
    expect(screen.getByLabelText("Загружаем избранное")).toHaveAttribute("aria-busy", "true");
    await act(async () => session.resolve(jsonResponse(sessionResponse())));
    expect(await screen.findByText("В избранном пока нет товаров")).toBeInTheDocument();
    expect(screen.getByText("Сохранено товаров: 0")).toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
  it("preserves server ordering, exact count, canonical links and out-of-stock content", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p2, p1)); page();
    expect(await screen.findByText("Сохранено товаров: 2")).toBeInTheDocument();
    const titles = screen.getAllByRole("heading", { level: 2 });
    expect(titles.map((h) => h.textContent)).toEqual([p2.name, p1.name]);
    expect(screen.getByRole("link", { name: p2.name })).toHaveAttribute("href", `/products/${p2.slug}`);
    expect(screen.getAllByText("Нет в наличии").length).toBeGreaterThan(0);
    expect(fetcher.mock.calls.map(([path]) => path)).toEqual(["/api/auth/session", "/api/favorites"]);
  });
  it("recovers keyboard focus to the next card and then the heading when empty", async () => {
    const user = userEvent.setup();
    respond(sessionResponse()); respond(favoriteResponse(p1, p2)); page();
    const first = await screen.findByRole("button", { name: `Избранное: ${p1.name}` });
    first.focus();
    respond(favoriteResponse(p2)); await user.keyboard("{Enter}");
    const second = screen.getByRole("button", { name: `Избранное: ${p2.name}` });
    await waitFor(() => expect(second).toHaveFocus());
    respond(favoriteResponse()); await user.keyboard(" ");
    await waitFor(() => expect(screen.getByRole("heading", { name: "Избранное", level: 1 })).toHaveFocus());
    expect(screen.getByText("В избранном пока нет товаров")).toBeInTheDocument();
  });
  it("a new anonymous server gate cannot flash a previously loaded account list", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1)); const { rerender } = page();
    await screen.findByRole("link", { name: p1.name });
    rerender(<FavoritesProvider><FavoritesView initialSession={{ status: "anonymous" }} /></FavoritesProvider>);
    expect(screen.queryByRole("link", { name: p1.name })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /Войдите/ })).toBeInTheDocument();
  });
});

describe("shared controls", () => {
  it("names anonymous navigation without a fake pressed state", async () => {
    respond({ authenticated: false });
    render(<FavoritesProvider><FavoriteButton productID="one" productName="Шоколад" /></FavoritesProvider>);
    const link = await screen.findByRole("link", { name: "Войдите, чтобы сохранить: Шоколад" });
    expect(link).toHaveAttribute("href", "/login?returnTo=%2F");
    expect(link).not.toHaveAttribute("aria-pressed");
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("anonymous text control is a sign-in link, never a toggle", async () => {
    respond({ authenticated: false });
    render(<FavoritesProvider><FavoriteButton productID="one" productName="Шоколад" text /></FavoritesProvider>);
    const link = await screen.findByRole("link", { name: "Войдите, чтобы сохранить: Шоколад" });
    expect(link).toHaveAttribute("href", "/login?returnTo=%2F");
    expect(link).toHaveTextContent("Войти, чтобы сохранить");
    expect(screen.queryByRole("button", { name: /Шоколад/ })).not.toBeInTheDocument();
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("comfortable and compact cards share state and keep controls outside product anchors", async () => {
    respond(sessionResponse()); respond(favoriteResponse(p1));
    const { container } = render(<FavoritesProvider><ProductCard product={p1} /><CompactProductCard product={p1} /></FavoritesProvider>);
    const buttons = await screen.findAllByRole("button", { name: `Избранное: ${p1.name}` });
    expect(buttons).toHaveLength(2);
    for (const button of buttons) { expect(button).toHaveAttribute("aria-pressed", "true"); expect(button.closest("a")).toBeNull(); }
    expect(container.querySelectorAll('button[aria-pressed="true"] svg[fill="currentColor"]')).toHaveLength(2);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});
