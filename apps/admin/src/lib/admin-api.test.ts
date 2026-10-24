import { describe, expect, it, vi } from "vitest";

import { adminRequest } from "./admin-api";
import { archiveProduct, createProduct, listProducts, updateProduct } from "./products-api";
import { product } from "@/test/api-mock";

const auth = { csrfToken: "csrf-9", staffId: "stf_" + "b".repeat(32) };

function stub(response: Response) {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("admin request layer", () => {
  it.each([
    [422, { error: { code: "validation_error", fields: [{ field: "slug", message: "x" }, { bad: true }] } }, { kind: "validation", fields: [{ field: "slug", message: "x" }] }],
    [409, { error: { code: "stale_revision" } }, { kind: "stale_revision" }],
    [409, { error: { code: "conflict" } }, { kind: "conflict" }],
    [409, { error: { code: "authenticated_staff_changed" } }, { kind: "staff_changed" }],
    [404, { error: { code: "not_found" } }, { kind: "not_found" }],
    [401, { error: { code: "unauthorized" } }, { kind: "unauthorized" }],
    [403, { error: { code: "csrf_failed" } }, { kind: "csrf" }],
    [503, "busy", { kind: "unavailable" }],
    [500, { error: { code: "internal_error", message: "pq: duplicate key", requestId: "r-1" } }, { kind: "failed", requestId: "r-1" }],
  ])("maps HTTP %i without exposing server text", async (status, body, error) => {
    const result = await adminRequest("/api/admin/v1/x", () => ({}), { fetcher: vi.fn<typeof fetch>().mockResolvedValue(Response.json(body, { status })) });
    expect(result).toEqual({ ok: false, error });
  });

  it("treats a network failure or a malformed success body as failures", async () => {
    expect(await adminRequest("/x", () => ({}), { fetcher: vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline")) })).toEqual({ ok: false, error: { kind: "unavailable" } });
    expect(await adminRequest("/x", () => null, { fetcher: vi.fn<typeof fetch>().mockResolvedValue(Response.json({})) })).toEqual({ ok: false, error: { kind: "failed" } });
  });

  it("sends the B1 query and documents with staff CSRF and expected staff ID", async () => {
    let fetcher = stub(Response.json({ items: [product()], total: 1, page: 2, limit: 25, totalPages: 1 }));
    expect((await listProducts({ q: "шоколад", status: "archived", page: 2, limit: 25 })).ok).toBe(true);
    expect(fetcher.mock.calls[0][0]).toBe("/api/admin/v1/products?status=archived&page=2&limit=25&q=%D1%88%D0%BE%D0%BA%D0%BE%D0%BB%D0%B0%D0%B4");

    fetcher = stub(Response.json(product({ revision: 8 })));
    const payload = { slug: "x" } as never;
    expect(await updateProduct("p 01", 7, payload, auth)).toEqual({ ok: true, data: product({ revision: 8 }) });
    const [url, init] = fetcher.mock.calls[0];
    const headers = new Headers(init!.headers);
    expect([url, init!.method]).toEqual(["/api/admin/v1/products/p%2001", "PUT"]);
    expect(JSON.parse(init!.body as string)).toEqual({ slug: "x", expectedRevision: 7 });
    expect(headers.get("X-CSRF-Token")).toBe("csrf-9");
    expect(headers.get("X-Expected-Staff-ID")).toBe(auth.staffId);
    expect(headers.has("X-Expected-Authenticated-User-ID") || headers.has("Authorization")).toBe(false);
    expect(init!.credentials).toBe("same-origin");

    fetcher = stub(Response.json(product({ archived: true, revision: 8 })));
    await archiveProduct("p01", 7, auth);
    expect([fetcher.mock.calls[0][0], fetcher.mock.calls[0][1]!.method, JSON.parse(fetcher.mock.calls[0][1]!.body as string)]).toEqual([
      "/api/admin/v1/products/p01/archive", "POST", { expectedRevision: 7 },
    ]);

    fetcher = stub(Response.json(product({ id: "prd_" + "c".repeat(32), revision: 1 })));
    await createProduct(payload, auth);
    expect([fetcher.mock.calls[0][0], fetcher.mock.calls[0][1]!.method]).toEqual(["/api/admin/v1/products", "POST"]);
    vi.unstubAllGlobals();
  });
});
