import { describe, expect, it, vi } from "vitest";

import { cartItem, cartResponse } from "@/test/cart-fixtures";
import { jsonResponse } from "@/test/favorites-fixtures";

import { BrowserRequestError, ResponseOwnerError } from "./browser-request";
import { getBrowserCart, mutateBrowserCart, type CartCommand } from "./cart-client";

function options(fetcher: ReturnType<typeof vi.fn>) {
  return { expectedUserID: "A", fetcher: fetcher as unknown as typeof fetch };
}

describe("browser cart transport", () => {
  it("GETs the complete same-origin cart without credentials in headers", async () => {
    const body = cartResponse(cartItem());
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(body));
    expect(await getBrowserCart(options(fetcher))).toEqual(body);
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/cart", expect.objectContaining({
      method: "GET", credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" },
    }));
  });

  it.each([
    [{ kind: "add", productID: "p/1", quantity: 3 }, "POST", "/api/cart/items", { productId: "p/1", quantity: 3 }, 201],
    [{ kind: "quantity", productID: "p/1", quantity: 5 }, "PATCH", "/api/cart/items/p%2F1", { quantity: 5 }, 200],
    [{ kind: "selected", productID: "p1", selected: false }, "PATCH", "/api/cart/items/p1", { selected: false }, 200],
    [{ kind: "remove", productID: "p1" }, "DELETE", "/api/cart/items/p1", undefined, 200],
    [{ kind: "selection", productIDs: ["p1", "p2"], selected: false }, "PATCH", "/api/cart/selection", { productIds: ["p1", "p2"], selected: false }, 200],
  ] as const)("sends the exact %j command once", async (command, method, path, body, status) => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(cartResponse(), status));
    await mutateBrowserCart(command as CartCommand, "current-csrf", options(fetcher));
    expect(fetcher).toHaveBeenCalledExactlyOnceWith(path, expect.objectContaining({ method, credentials: "same-origin", cache: "no-store" }));
    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect(init.body).toBe(body === undefined ? undefined : JSON.stringify(body));
    expect(init.headers).toEqual({
      Accept: "application/json", "X-CSRF-Token": "current-csrf",
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    });
  });

  it("never uses bulk delete or query-scoped selection", async () => {
    const fetcher = vi.fn().mockImplementation(async () => jsonResponse(cartResponse()));
    await mutateBrowserCart({ kind: "selection", productIDs: ["p1"], selected: true }, "csrf", options(fetcher));
    await mutateBrowserCart({ kind: "remove", productID: "p1" }, "csrf", options(fetcher));
    for (const [path, init] of fetcher.mock.calls) {
      expect(String(path)).not.toMatch(/^\/api\/cart(\?|$)/);
      expect(String(path)).not.toContain("selectedOnly");
      expect((init as RequestInit).method).not.toBe("PUT");
    }
  });

  it.each([
    ["empty", []],
    ["blank", [""]],
    ["whitespace", [" "]],
    ["duplicate", ["p1", "p1"]],
    ["null", null],
    ["omitted", undefined],
  ])("rejects %s bulk selection IDs before any request", async (_name, productIDs) => {
    const fetcher = vi.fn();
    const command = { kind: "selection", productIDs, selected: true } as unknown as CartCommand;
    await expect(mutateBrowserCart(command, "csrf", options(fetcher))).rejects.toThrow(/explicit nonempty/);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it.each(["B", null, ""])("rejects owner %j before reading the private body", async (owner) => {
    const response = jsonResponse(cartResponse(cartItem()), 200, owner);
    const json = vi.spyOn(response, "json");
    await expect(getBrowserCart(options(vi.fn().mockResolvedValue(response)))).rejects.toBeInstanceOf(ResponseOwnerError);
    expect(json).not.toHaveBeenCalled();
  });

  it("requires 201 for POST; an unexpected success status is an uncertain failure", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse(cartResponse()));
    await expect(mutateBrowserCart({ kind: "add", productID: "p1", quantity: 3 }, "csrf", options(fetcher)))
      .rejects.toMatchObject({ status: 200 });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it.each([
    [409, "out_of_stock", { message: "Недостаточно товара" }, "Недостаточно товара"],
    [422, "validation_error", { fields: [{ field: "quantity", message: "Шаг 2" }] }, "Шаг 2"],
    [404, "not_found", {}, undefined],
    [403, "csrf_failed", {}, undefined],
  ])("preserves %s %s without retrying", async (status, code, extra, detail) => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ error: { code, ...extra } }, status));
    const result = mutateBrowserCart({ kind: "quantity", productID: "p1", quantity: 4 }, "csrf", options(fetcher));
    await expect(result).rejects.toBeInstanceOf(BrowserRequestError);
    await expect(result).rejects.toMatchObject({ status, code, detail });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("a malformed successful cart is an error, never a partial snapshot", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ items: [] }, 201));
    await expect(mutateBrowserCart({ kind: "add", productID: "p1", quantity: 1 }, "csrf", options(fetcher))).rejects.toThrow();
  });
});
