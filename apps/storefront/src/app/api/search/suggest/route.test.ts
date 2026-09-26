// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { GET } from "./route";

const upstream = vi.fn<typeof fetch>();

function upstreamJson(body: unknown, status = 200, requestId = "go-request") {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", "X-Request-ID": requestId },
  });
}

function suggest(query: string, init: RequestInit = {}) {
  return GET(
    new Request(`http://storefront.test/api/search/suggest?${new URLSearchParams({ q: query })}`, init),
  );
}

beforeEach(() => {
  vi.stubGlobal("fetch", upstream);
});

afterEach(() => {
  upstream.mockReset();
  vi.unstubAllGlobals();
});

describe("suggestion BFF route", () => {
  it.each(["", "   ", "ш", " ш "])("returns empty items for %j without an upstream request", async (query) => {
    const response = await suggest(query);

    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toBe("no-store");
    await expect(response.json()).resolves.toEqual({ items: [] });
    expect(upstream).not.toHaveBeenCalled();
  });

  it("returns Go's 422 validation envelope above 200 code points without an upstream request", async () => {
    const response = await suggest("ш".repeat(201), { headers: { "X-Request-ID": "client-request" } });

    expect(response.status).toBe(422);
    expect(response.headers.get("x-request-id")).toBe("client-request");
    expect(response.headers.get("cache-control")).toBe("no-store");
    await expect(response.json()).resolves.toEqual({
      error: {
        code: "validation_error",
        message: "Проверьте заполненные поля",
        requestId: "client-request",
        fields: [{ field: "q", message: "Поисковый запрос не должен быть длиннее 200 символов" }],
      },
    });
    expect(upstream).not.toHaveBeenCalled();
  });

  it("generates a request ID for locally rejected queries when none is supplied", async () => {
    const response = await suggest("a".repeat(201));
    const body = await response.json();

    expect(response.status).toBe(422);
    expect(body.error.requestId).toMatch(/\S/);
    expect(response.headers.get("x-request-id")).toBe(body.error.requestId);
  });

  it("forwards exactly 200 astral code points, which is 400 UTF-16 units", async () => {
    upstream.mockResolvedValue(upstreamJson({ items: [] }));
    const query = "🍫".repeat(200);

    const response = await suggest(query);

    expect(response.status).toBe(200);
    expect(upstream).toHaveBeenCalledTimes(1);
    expect(String(upstream.mock.calls[0][0])).toBe(
      `http://127.0.0.1:8080/api/search/suggest?${new URLSearchParams({ q: query })}`,
    );
  });

  it("forwards trimmed queries, validates the payload and propagates cancellation", async () => {
    upstream.mockResolvedValue(
      upstreamJson({
        items: [
          { type: "product", id: "p01", slug: "dark-chocolate-sea-salt", title: "Тёмный шоколад", subtitle: "Choco Atelier · плитка 90 г" },
          { type: "series", id: "s", slug: "s", title: "Future type" },
        ],
      }),
    );
    const controller = new AbortController();

    const response = await suggest("  шоколад ", { signal: controller.signal });

    expect(response.status).toBe(200);
    expect(response.headers.get("x-request-id")).toBe("go-request");
    await expect(response.json()).resolves.toEqual({
      items: [
        { type: "product", id: "p01", slug: "dark-chocolate-sea-salt", title: "Тёмный шоколад", subtitle: "Choco Atelier · плитка 90 г" },
      ],
    });
    const [url, init] = upstream.mock.calls[0];
    expect(String(url)).toContain(`?${new URLSearchParams({ q: "шоколад" })}`);
    expect(init?.signal?.aborted).toBe(false);
    controller.abort();
    expect(init?.signal?.aborted).toBe(true);
  });

  it("keeps Go's 422 verdict when upstream still rejects the query", async () => {
    upstream.mockResolvedValue(upstreamJson({ error: { code: "validation_error" } }, 422, "go-422"));

    const response = await suggest("﻿" + "a".repeat(200));

    expect(response.status).toBe(422);
    const body = await response.json();
    expect(body.error).toMatchObject({ code: "validation_error", requestId: "go-422" });
    expect(body.error.fields).toEqual([
      { field: "q", message: "Поисковый запрос не должен быть длиннее 200 символов" },
    ]);
  });

  it.each([
    ["an upstream error", () => upstream.mockResolvedValue(upstreamJson({ error: {} }, 500))],
    ["an unreachable upstream", () => upstream.mockRejectedValue(new TypeError("fetch failed"))],
    ["a malformed upstream body", () => upstream.mockResolvedValue(upstreamJson({ items: [{ type: "product" }] }))],
  ])("maps %s to a schema-valid 502 without internals", async (_name, arrange) => {
    arrange();

    const response = await suggest("шоколад", { headers: { "X-Request-ID": "client-request" } });

    expect(response.status).toBe(502);
    const body = await response.json();
    expect(Object.keys(body.error).sort()).toEqual(["code", "message", "requestId"]);
    expect(body.error).toMatchObject({ code: "internal_error", message: "Не удалось выполнить запрос" });
  });
});
