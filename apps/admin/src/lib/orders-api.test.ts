import { afterEach, describe, expect, it, vi } from "vitest";

import { order } from "@/test/api-mock";
import { orderListHref, parseOrderQuery } from "./order-query";
import { cancelOrder, changeOrderStatus, listOrders, parseOrder } from "./orders-api";

const auth = { csrfToken: "csrf-7", staffId: "stf_" + "d".repeat(32) };

function stub(response: Response) {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("order API client", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("sends only the lifecycle requests Go offers, with staff credentials", async () => {
    let fetcher = stub(Response.json(order({ status: "transit", revision: 2, allowedTransitions: ["delivered"] })));
    await changeOrderStatus("ord-2026-10483", 1, "transit", auth);
    let [url, init] = fetcher.mock.calls[0];
    expect([url, init!.method, JSON.parse(init!.body as string)]).toEqual(["/api/admin/v1/orders/ord-2026-10483/status", "POST", { status: "transit", expectedRevision: 1 }]);
    expect(new Headers(init!.headers).get("X-CSRF-Token")).toBe("csrf-7");
    expect(new Headers(init!.headers).get("X-Expected-Staff-ID")).toBe(auth.staffId);

    fetcher = stub(Response.json(order({ status: "cancelled", revision: 2, allowedTransitions: [] })));
    await cancelOrder("ord-2026-10483", 1, auth);
    [url, init] = fetcher.mock.calls[0];
    expect([url, init!.method, JSON.parse(init!.body as string)]).toEqual(["/api/admin/v1/orders/ord-2026-10483/cancel", "POST", { expectedRevision: 1 }]);

    fetcher = stub(Response.json({ items: [order({ items: undefined })], total: 1, page: 1, limit: 25, totalPages: 1 }));
    expect((await listOrders({ q: "#10483", status: "processing", page: 1, limit: 25 })).ok).toBe(true);
    expect(fetcher.mock.calls[0][0]).toBe("/api/admin/v1/orders?page=1&limit=25&status=processing&q=%2310483");
  });

  it("maps invalid_transition to its own failure", async () => {
    stub(Response.json({ error: { code: "invalid_transition", message: "x", requestId: "r" } }, { status: 409 }));
    expect(await cancelOrder("ord-1", 1, auth)).toEqual({ ok: false, error: { kind: "invalid_transition" } });
  });

  it("rejects malformed orders and unknown transitions", () => {
    expect(parseOrder(order())).not.toBeNull();
    expect(parseOrder(order({ allowedTransitions: ["refunded" as never] }))).toBeNull();
    expect(parseOrder({ ...order(), customer: { name: "x" } })).toBeNull();
    expect(parseOrder({ ...order(), totalMinor: 1.5 })).toBeNull();
  });

  it("keeps the order list state in the URL", () => {
    expect(parseOrderQuery({ status: "cancelled", q: " #10483 ", page: "2", limit: "100" })).toEqual({ q: "#10483", status: "cancelled", page: 2, limit: 100 });
    expect(parseOrderQuery({ status: "refunded", page: "0" })).toEqual({ q: "", status: "", page: 1, limit: 50 });
    expect(orderListHref({ q: "", status: "", page: 1, limit: 50 })).toBe("/orders");
    expect(orderListHref({ q: "#1", status: "transit", page: 3, limit: 25 })).toBe("/orders?q=%231&status=transit&page=3&limit=25");
  });
});
