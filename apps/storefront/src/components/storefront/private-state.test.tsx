import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode, useEffect, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({ usePathname: () => "/", useRouter: () => ({ push: vi.fn() }) }));

import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { deferred, favoriteProduct, favoriteResponse, jsonResponse, sessionResponse } from "@/test/favorites-fixtures";
import { PrivateProviders } from "@/test/private-providers";

import { CartControl } from "./cart-control";
import { AccountView } from "./account-view";
import { useCart } from "./cart-provider";
import { FavoriteButton } from "./favorite-button";
import { useFavorites } from "./favorites-provider";
import { StorefrontHeader } from "./header";
import { MobileTabBar } from "./mobile-tab-bar";
import { SiteFooter } from "./site-footer";

const fetcher = vi.fn<typeof fetch>();
const product = favoriteProduct();

function CartProbe({ initialize = true }: { initialize?: boolean }) {
  const state = useCart();
  useEffect(() => { if (initialize) void state.ensure(); }, [initialize, state.ensure]);
  return <>
    <div data-testid="cart">{JSON.stringify({
      status: state.status, ids: state.cart?.items.map((line) => line.product.id) ?? null,
      qty: state.cart?.items[0]?.quantity ?? null, pending: state.mutationPending, reconciling: state.reconciling,
    })}</div>
    <button onClick={() => void state.refresh()}>refresh-cart</button>
    <button onClick={() => void state.mutate({ kind: "add", productID: "p01", quantity: 1 })}>write-cart</button>
    <button onClick={() => {
      void state.mutate({ kind: "quantity", productID: "p01", quantity: 2 });
      void state.mutate({ kind: "selected", productID: "p02", selected: false });
    }}>two-cart</button>
    <button onClick={() => void state.selectAll(false)}>bulk-cart</button>
  </>;
}

function FavoritesProbe({ initialize = true }: { initialize?: boolean }) {
  const state = useFavorites();
  useEffect(() => { if (initialize) void state.ensure(); }, [initialize, state.ensure]);
  return <>
    <div data-testid="favorites">{JSON.stringify({ status: state.status, ids: state.favorites?.items.map((p) => p.id) ?? null })}</div>
    <button onClick={() => void state.refresh()}>refresh-favorites</button>
    <button onClick={() => void state.toggle("p01")}>write-favorites</button>
  </>;
}

type Feature = "cart" | "favorites";
function mount(feature: Feature | "both" = "both") {
  return render(
    <PrivateProviders>
      <FavoritesProbe initialize={feature !== "cart"} />
      <CartProbe initialize={feature !== "favorites"} />
    </PrivateProviders>,
  );
}
function view(feature: Feature) { return JSON.parse(screen.getByTestId(feature).textContent!); }
function requests() { return fetcher.mock.calls.map(([path, init]) => `${init?.method} ${path}`); }
function count(request: string) { return requests().filter((value) => value === request).length; }
function respond(body: unknown, status = 200, owner: string | null = "A") {
  fetcher.mockResolvedValueOnce(jsonResponse(body, status, owner));
}
function body(feature: Feature, populated = false) {
  return feature === "cart" ? cartResponse(...(populated ? [cartItem()] : [])) : favoriteResponse(...(populated ? [product] : []));
}
async function ready(feature: Feature, ids: string[] = []) {
  await waitFor(() => expect(view(feature)).toMatchObject({ status: "ready", ids }));
}
// Answers every read for the authenticated user (or anonymous) by path.
function serve(user: string | null = "A") {
  fetcher.mockImplementation(async (path) => {
    if (String(path) === "/api/auth/session") return jsonResponse(user ? sessionResponse(user) : { authenticated: false }, 200, null);
    return jsonResponse(String(path) === "/api/cart" ? cartResponse() : favoriteResponse(), 200, user);
  });
}

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe("shared private initialization", () => {
  it.each([false, true])("confirmed account logout clears populated Cart/Favorites (late reads=%s)", async (late) => {
    fetcher.mockImplementation(async (path) => {
      if (String(path) === "/api/auth/session") return jsonResponse(sessionResponse(), 200, null);
      return jsonResponse(String(path) === "/api/cart" ? cartResponse(cartItem()) : favoriteResponse(product));
    });
    render(<PrivateProviders>
      <AccountView initialSession={{ status: "authenticated", userID: "A" }} />
      <CartProbe /><FavoritesProbe />
    </PrivateProviders>);
    await ready("cart", ["p01"]);
    await ready("favorites", ["p01"]);
    const lateCart = deferred<Response>();
    const lateFavorites = deferred<Response>();
    if (late) {
      fetcher.mockImplementation((path) => {
        if (String(path) === "/api/auth/session") return Promise.resolve(jsonResponse(sessionResponse(), 200, null));
        return String(path) === "/api/cart" ? lateCart.promise : lateFavorites.promise;
      });
      fireEvent.click(screen.getByText("refresh-cart"));
      await screen.findByRole("button", { name: "Выйти" });
      await waitFor(() => expect(count("GET /api/favorites")).toBe(2));
    }
    fetcher.mockResolvedValueOnce(new Response(null, { status: 204, headers: { "X-Authenticated-User-ID": "A" } }));
    fireEvent.click(screen.getByRole("button", { name: "Выйти" }));
    await screen.findByRole("heading", { name: "Вы вышли из аккаунта" });
    expect(view("cart")).toMatchObject({ status: "anonymous", ids: null });
    expect(view("favorites")).toMatchObject({ status: "anonymous", ids: null });
    const requestCount = fetcher.mock.calls.length;
    await act(async () => {
      lateCart.resolve(jsonResponse(cartResponse(cartItem())));
      lateFavorites.resolve(jsonResponse(favoriteResponse(product)));
    });
    expect(view("cart")).toMatchObject({ status: "anonymous", ids: null });
    expect(view("favorites")).toMatchObject({ status: "anonymous", ids: null });
    expect(fetcher).toHaveBeenCalledTimes(requestCount);
    expect(count("DELETE /api/auth/session")).toBe(1);
    expect(count("GET /api/auth/session")).toBe(late ? 2 : 1);
  });

  it.each([true, false])("Favorites and Cart controls share one session request (signed in=%s)", async (signedIn) => {
    serve(signedIn ? "A" : null);
    const controls = <>
      <FavoriteButton productID="p01" productName="One" /><CartControl product={product} />
      <FavoriteButton productID="p02" productName="Two" /><CartControl product={favoriteProduct("p02")} />
    </>;
    const { rerender } = render(<StrictMode><PrivateProviders>{controls}</PrivateProviders></StrictMode>);
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(signedIn ? 3 : 1));
    // More cards mounting later never re-initialize.
    rerender(<StrictMode><PrivateProviders>{controls}
      <CartControl product={favoriteProduct("p03")} /><FavoriteButton productID="p03" productName="Three" />
    </PrivateProviders></StrictMode>);
    await act(async () => {});
    expect(count("GET /api/auth/session")).toBe(1);
    expect(count("GET /api/favorites")).toBe(signedIn ? 1 : 0);
    expect(count("GET /api/cart")).toBe(signedIn ? 1 : 0);
    expect(requests().some((request) => request.includes("/api/products"))).toBe(false);
  });

  it("the header, footer and tab bar never initialize the session, Favorites or Cart", async () => {
    render(<PrivateProviders><StorefrontHeader /><SiteFooter /><MobileTabBar /></PrivateProviders>);
    await act(async () => {});
    // Header action, footer link and tab: one each, all plain links.
    expect(screen.getAllByRole("link", { name: "Корзина" })).toHaveLength(3);
    expect(screen.getAllByRole("link", { name: "Аккаунт" })).toHaveLength(3);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("one visibility or BFCache restoration revalidates once, then rereads both features", async () => {
    serve();
    mount();
    await ready("cart");
    await ready("favorites");
    expect(fetcher).toHaveBeenCalledTimes(3);
    const visibility = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    act(() => { document.dispatchEvent(new Event("visibilitychange")); });
    visibility.mockReturnValue("visible");
    act(() => { document.dispatchEvent(new Event("visibilitychange")); });
    // A BFCache restore inside the same throttle window adds nothing.
    act(() => { window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })); });
    await ready("cart");
    await ready("favorites");
    expect(count("GET /api/auth/session")).toBe(2);
    expect(count("GET /api/cart")).toBe(2);
    expect(count("GET /api/favorites")).toBe(2);
  });

  it("401 from a Cart write hides Cart and Favorites without replay", async () => {
    serve();
    mount();
    await ready("cart");
    await ready("favorites");
    respond({ error: { code: "unauthorized" } }, 401, null);
    fireEvent.click(screen.getByText("write-cart"));
    await waitFor(() => expect(view("cart").status).toBe("anonymous"));
    expect(view("favorites")).toMatchObject({ status: "anonymous", ids: null });
    expect(requests().filter((request) => !request.startsWith("GET"))).toEqual(["POST /api/cart/items"]);
    expect(screen.getByRole("status", { name: "Уведомления корзины" })).toHaveTextContent("Сессия завершена");
  });

  it("csrf_failed refreshes the shared session and rereads initialized features, never replaying", async () => {
    serve();
    mount();
    await ready("cart");
    await ready("favorites");
    respond({ error: { code: "csrf_failed" } }, 403, null);
    fireEvent.click(screen.getByText("write-cart"));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(7));
    await ready("cart");
    await ready("favorites");
    expect(count("POST /api/cart/items")).toBe(1);
    expect(count("GET /api/auth/session")).toBe(2);
    expect(screen.getByRole("status", { name: "Уведомления корзины" })).toHaveTextContent("повторите действие");
  });

  it("no session, CSRF or private data is written to browser storage", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    serve();
    mount();
    await ready("cart");
    await ready("favorites");
    respond(cartResponse(cartItem()), 201);
    fireEvent.click(screen.getByText("write-cart"));
    await ready("cart", ["p01"]);
    expect(setItem).not.toHaveBeenCalled();
    expect(window.localStorage.length + window.sessionStorage.length).toBe(0);
    for (const [, init] of fetcher.mock.calls) {
      expect(init?.headers).not.toHaveProperty("Authorization");
      expect(init).toMatchObject({ credentials: "same-origin", cache: "no-store" });
    }
  });
});

describe.each(["cart", "favorites"] as const)("%s response owner binding", (feature) => {
  it("accepts a same-owner response", async () => {
    respond(sessionResponse(), 200, null);
    respond(body(feature, true));
    mount(feature);
    await ready(feature, ["p01"]);
  });

  it.each(["B", null, ""])("rejects a GET owned by %j and refreshes once", async (owner) => {
    respond(sessionResponse(), 200, null);
    respond(body(feature, true), 200, owner);
    respond(sessionResponse("B"), 200, null);
    respond(body(feature), 200, "B");
    mount(feature);
    await ready(feature);
    expect(view(feature).ids).toEqual([]);
    expect(requests()).toEqual(["GET /api/auth/session", `GET /api/${feature}`, "GET /api/auth/session", `GET /api/${feature}`]);
  });

  it("persistent mismatch settles unavailable without a loop; deliberate retry recovers", async () => {
    respond(sessionResponse(), 200, null);
    respond(body(feature, true), 200, "B");
    respond(sessionResponse(), 200, null);
    respond(body(feature, true), 200, null);
    mount(feature);
    await waitFor(() => expect(view(feature).status).toBe("unavailable"));
    await act(async () => {});
    expect(view(feature).ids).toBeNull();
    expect(fetcher).toHaveBeenCalledTimes(4);
    respond(sessionResponse(), 200, null);
    respond(body(feature));
    fireEvent.click(screen.getByText(`refresh-${feature}`));
    await ready(feature);
  });

  it("a B-owned mutation response is discarded and only reads follow", async () => {
    respond(sessionResponse(), 200, null);
    respond(body(feature));
    mount(feature);
    await ready(feature);
    respond(body(feature, true), feature === "cart" ? 201 : 200, "B");
    respond(sessionResponse("B"), 200, null);
    respond(body(feature), 200, "B");
    fireEvent.click(screen.getByText(`write-${feature}`));
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(5));
    await ready(feature);
    expect(view(feature).ids).toEqual([]);
    expect(requests().filter((request) => !request.startsWith("GET"))).toHaveLength(1);
  });

  it("an obsolete-generation owner mismatch never starts recovery", async () => {
    const old = deferred<Response>();
    respond(sessionResponse(), 200, null);
    fetcher.mockReturnValueOnce(old.promise);
    mount(feature);
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    respond(sessionResponse("B"), 200, null);
    respond(body(feature), 200, "B");
    fireEvent.click(screen.getByText(`refresh-${feature}`));
    await ready(feature);
    await act(async () => old.resolve(jsonResponse(body(feature, true), 200, "wrong")));
    expect(fetcher).toHaveBeenCalledTimes(4);
    expect(view(feature).ids).toEqual([]);
  });
});

describe("cart sequencing and failures", () => {
  it("one global write lock: a second product or bulk write is dropped, not queued", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse(cartItem(), cartItem("p02")));
    mount("cart");
    await ready("cart", ["p01", "p02"]);
    const write = deferred<Response>();
    fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(screen.getByText("two-cart"));
    fireEvent.click(screen.getByText("bulk-cart"));
    expect(fetcher).toHaveBeenCalledTimes(3);
    // Non-optimistic: nothing changes until Go confirms.
    expect(view("cart")).toMatchObject({ qty: 1, pending: true });
    await act(async () => write.resolve(jsonResponse(cartResponse(cartItem("p01", 2), cartItem("p02")))));
    await waitFor(() => expect(view("cart").qty).toBe(2));
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it.each(["network", "500", "malformed", "unexpected status"])("%s write reconciles with a read-only GET", async (failure) => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    mount("cart");
    await ready("cart");
    if (failure === "network") fetcher.mockRejectedValueOnce(new TypeError("offline"));
    else if (failure === "500") respond({ error: { code: "internal_error" } }, 500);
    else if (failure === "malformed") respond({}, 201);
    else respond(cartResponse(cartItem()), 200);
    const reconciliation = deferred<Response>();
    fetcher.mockReturnValueOnce(reconciliation.promise);
    fireEvent.click(screen.getByText("write-cart"));
    // The last confirmed snapshot stays visible but marked as being checked.
    await waitFor(() => expect(view("cart")).toMatchObject({ status: "ready", ids: [], reconciling: true, pending: true }));
    await act(async () => reconciliation.resolve(jsonResponse(cartResponse(cartItem()))));
    await ready("cart", ["p01"]);
    expect(view("cart")).toMatchObject({ reconciling: false, pending: false });
    expect(requests().slice(2)).toEqual(["POST /api/cart/items", "GET /api/cart"]);
    expect(screen.getByRole("status", { name: "Уведомления корзины" })).toHaveTextContent("Показана сохранённая корзина");
    expect(screen.getByRole("status", { name: "Уведомления корзины" })).not.toHaveTextContent(/отмен/i);
  });

  it("failed reconciliation blocks writes until deliberate recovery", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    mount("cart");
    await ready("cart");
    respond({}, 500, null);
    respond({}, 500, null);
    fireEvent.click(screen.getByText("write-cart"));
    await waitFor(() => expect(view("cart")).toMatchObject({ status: "unavailable", ids: null }));
    fireEvent.click(screen.getByText("write-cart"));
    expect(fetcher).toHaveBeenCalledTimes(4);
    respond(sessionResponse(), 200, null);
    respond(cartResponse(cartItem()));
    fireEvent.click(screen.getByText("refresh-cart"));
    await ready("cart", ["p01"]);
    expect(count("POST /api/cart/items")).toBe(1);
  });

  it.each(["anonymous", "unavailable", "B"])("A -> %s hides A's Cart synchronously and ignores the late write", async (destination) => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    mount("cart");
    await ready("cart");
    const write = deferred<Response>();
    fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(screen.getByText("write-cart"));
    respond(destination === "B" ? sessionResponse("B") : destination === "anonymous" ? { authenticated: false } : {}, 200, null);
    if (destination === "B") respond(cartResponse(), 200, "B");
    fireEvent.click(screen.getByText("refresh-cart"));
    expect(view("cart").ids).toBeNull();
    if (destination === "B") await ready("cart");
    else await waitFor(() => expect(view("cart").status).toBe(destination));
    await act(async () => write.resolve(jsonResponse(cartResponse(cartItem()), 201)));
    expect(view("cart").ids).toEqual(destination === "B" ? [] : null);
    expect(count("POST /api/cart/items")).toBe(1);
  });

  it.each([404, 409, 422])("%s keeps the confirmed Cart and never replays", async (status) => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse(cartItem()));
    mount("cart");
    await ready("cart", ["p01"]);
    respond({ error: { code: status === 409 ? "out_of_stock" : status === 404 ? "not_found" : "validation_error" } }, status);
    if (status !== 422) respond(cartResponse(cartItem()));
    fireEvent.click(screen.getByText("two-cart"));
    await waitFor(() => expect(view("cart").pending).toBe(false));
    expect(view("cart").qty).toBe(1);
    expect(requests().slice(2)).toEqual(status === 422
      ? ["PATCH /api/cart/items/p01"]
      : ["PATCH /api/cart/items/p01", "GET /api/cart"]);
  });

  it("an empty Cart never sends bulk selection", async () => {
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    mount("cart");
    await ready("cart");
    fireEvent.click(screen.getByText("bulk-cart"));
    await act(async () => {});
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("soft navigation during a write keeps the provider-owned result without extra requests", async () => {
    function Route() {
      const [page, setPage] = useState<"product" | "other">("product");
      return <>
        <button onClick={() => setPage("other")}>navigate</button>
        {page === "product" ? <CartControl product={product} /> : <p>Другая страница</p>}
      </>;
    }
    respond(sessionResponse(), 200, null);
    respond(cartResponse());
    render(<PrivateProviders><Route /><CartProbe initialize={false} /></PrivateProviders>);
    const add = await screen.findByRole("button", { name: `Добавить в корзину: ${product.name}` });
    await waitFor(() => expect(add).toHaveAttribute("aria-disabled", "false"));
    const write = deferred<Response>();
    fetcher.mockReturnValueOnce(write.promise);
    fireEvent.click(add);
    fireEvent.click(screen.getByText("navigate"));
    await act(async () => write.resolve(jsonResponse(cartResponse(cartItem()), 201)));
    await ready("cart", ["p01"]);
    expect(requests()).toEqual(["GET /api/auth/session", "GET /api/cart", "POST /api/cart/items"]);
  });
});
