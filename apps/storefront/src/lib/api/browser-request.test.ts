import { describe, expect, it, vi } from "vitest";

import { jsonResponse } from "@/test/favorites-fixtures";

import { browserNoContent, browserRequest, BrowserRequestError, ResponseOwnerError } from "./browser-request";
import { logoutBrowserSession } from "./auth-client";

function request(response: Response, expectedUserID = "A") {
  return browserRequest("/api/cart", { expectedUserID, fetcher: vi.fn().mockResolvedValue(response) });
}

describe("explicit no-content transport", () => {
  function empty(owner: string | null = "A") {
    return new Response(null, { status: 204, headers: owner === null ? {} : { "X-Authenticated-User-ID": owner } });
  }

  it("accepts only the explicit 204 path and sends guarded cookie DELETE", async () => {
    const response = empty();
    const json = vi.spyOn(response, "json");
    const fetcher = vi.fn().mockResolvedValue(response);
    await expect(logoutBrowserSession({ expectedUserID: "A", csrfToken: "csrf-A", fetcher })).resolves.toBeUndefined();
    expect(json).not.toHaveBeenCalled();
    expect(fetcher).toHaveBeenCalledExactlyOnceWith("/api/auth/session", expect.objectContaining({
      method: "DELETE", credentials: "same-origin", cache: "no-store",
      headers: { Accept: "application/json", "X-CSRF-Token": "csrf-A", "X-Expected-Authenticated-User-ID": "A" },
    }));
  });

  it("keeps JSON endpoints strict even with success: 204", async () => {
    await expect(browserRequest("/api/cart", { expectedUserID: "A", success: 204, fetcher: vi.fn().mockResolvedValue(empty()) }))
      .rejects.toBeInstanceOf(BrowserRequestError);
    await expect(request(new Response("", { status: 200, headers: { "X-Authenticated-User-ID": "A" } })))
      .rejects.toBeInstanceOf(BrowserRequestError);
  });

  it.each([null, "", "B"])("rejects a 204 with owner %j before reading anything", async (owner) => {
    const response = empty(owner);
    const json = vi.spyOn(response, "json");
    await expect(browserNoContent("/api/auth/session", { expectedUserID: "A", fetcher: vi.fn().mockResolvedValue(response) }))
      .rejects.toBeInstanceOf(ResponseOwnerError);
    expect(json).not.toHaveBeenCalled();
  });

  it.each([200, 201, 202, 205, 500])("does not accept unexpected %s", async (status) => {
    await expect(logoutBrowserSession({ expectedUserID: "A", csrfToken: "csrf", fetcher: vi.fn().mockResolvedValue((status === 205 ? new Response(null, { status, headers: { "X-Authenticated-User-ID": "A" } }) : jsonResponse({}, status))) }))
      .rejects.toMatchObject({ status });
  });

  it("rejects malformed responses", async () => {
    await expect(logoutBrowserSession({ expectedUserID: "A", csrfToken: "csrf", fetcher: vi.fn().mockResolvedValue(new Response("not-json", { headers: { "X-Authenticated-User-ID": "A" } })) }))
      .rejects.toBeInstanceOf(BrowserRequestError);
  });

  it("preserves timeout and network errors without retry", async () => {
    const network = vi.fn().mockRejectedValue(new TypeError("offline"));
    await expect(logoutBrowserSession({ expectedUserID: "A", csrfToken: "csrf", fetcher: network })).rejects.toBeInstanceOf(BrowserRequestError);
    expect(network).toHaveBeenCalledOnce();
    const timeout = vi.fn<typeof fetch>().mockImplementation((_path, init) => new Promise((_resolve, reject) => {
      init!.signal!.addEventListener("abort", () => reject(init!.signal!.reason));
    }));
    await expect(logoutBrowserSession({ expectedUserID: "A", csrfToken: "csrf", timeoutMs: 5, fetcher: timeout })).rejects.toBeInstanceOf(BrowserRequestError);
    expect(timeout).toHaveBeenCalledOnce();
  });
});

describe("private response owner binding", () => {
  it("accepts a body bound to the expected user", async () => {
    await expect(request(jsonResponse({ ok: true }, 200, "A"))).resolves.toEqual({ ok: true });
  });

  it.each([
    [200, "B"], [200, null], [200, ""], [201, null],
    // Downstream errors are produced after authentication and must be bound too.
    [404, null], [409, "B"], [422, null], [500, "B"],
  ])("fails closed for %s with owner %j", async (status, owner) => {
    await expect(request(jsonResponse({ error: { code: "x" } }, status, owner))).rejects.toBeInstanceOf(ResponseOwnerError);
  });

  it("an empty expected owner never matches an empty header", async () => {
    await expect(request(jsonResponse({}, 200, ""), "")).rejects.toBeInstanceOf(ResponseOwnerError);
  });

  it.each([
    [401, "unauthorized"], [403, "csrf_failed"],
  ])("pre-authentication %s carries no owner and keeps its code", async (status, code) => {
    const result = request(jsonResponse({ error: { code } }, status, null));
    await expect(result).rejects.not.toBeInstanceOf(ResponseOwnerError);
    await expect(result).rejects.toMatchObject({ status, code });
  });

  it("an unbound 5xx (session lookup failure) is an uncertain error, not private data", async () => {
    const result = request(jsonResponse({ error: { code: "internal_error" } }, 500, null));
    await expect(result).rejects.toBeInstanceOf(BrowserRequestError);
    await expect(result).rejects.not.toBeInstanceOf(ResponseOwnerError);
  });

  it("ignores spoofable request input: only the response header is compared", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({}, 200, "A"));
    await browserRequest("/api/cart", { expectedUserID: "A", fetcher });
    const init = fetcher.mock.calls[0][1] as RequestInit;
    expect(init.headers).not.toHaveProperty("X-Authenticated-User-ID");
    expect(init.headers).not.toHaveProperty("Authorization");
    expect(init).toMatchObject({ credentials: "same-origin", cache: "no-store" });
  });
});

describe("request-side expected user", () => {
  function sentHeaders(fetcher: ReturnType<typeof vi.fn>) {
    return (fetcher.mock.calls[0][1] as RequestInit).headers as Record<string, string>;
  }

  it("sends the expected user with every private request", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({}, 201, "A"));
    await browserRequest("/api/orders", { expectedUserID: "A", method: "POST", body: {}, csrfToken: "csrf", success: 201, fetcher });
    expect(sentHeaders(fetcher)).toEqual({
      Accept: "application/json", "X-CSRF-Token": "csrf", "Content-Type": "application/json",
      "X-Expected-Authenticated-User-ID": "A",
    });
  });

  it.each([undefined, ""])("omits it without a nonempty expected user (%j)", async (expectedUserID) => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ authenticated: false }, 200, null));
    await browserRequest("/api/auth/session", { expectedUserID, fetcher }).catch(() => undefined);
    expect(sentHeaders(fetcher)).not.toHaveProperty("X-Expected-Authenticated-User-ID");
  });

  it("callers cannot add or override protected headers", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({}, 200, "A"));
    const untyped = {
      expectedUserID: "A", fetcher,
      headers: { "X-Expected-Authenticated-User-ID": "B", Authorization: "Bearer x", "X-Authenticated-User-ID": "B" },
    } as unknown as Parameters<typeof browserRequest>[1];
    await browserRequest("/api/cart", untyped);
    expect(sentHeaders(fetcher)).toEqual({ Accept: "application/json", "X-Expected-Authenticated-User-ID": "A" });
  });

  it("Go's 409 authenticated_user_changed names another owner and is rejected before the body is read", async () => {
    const response = jsonResponse({ error: { code: "authenticated_user_changed" } }, 409, "B");
    const json = vi.spyOn(response, "json");
    await expect(request(response)).rejects.toBeInstanceOf(ResponseOwnerError);
    expect(json).not.toHaveBeenCalled();
  });

  it("keeps validation fields for the caller", async () => {
    const response = jsonResponse({ error: { code: "validation_error", message: "x", fields: [{ field: "deliveryAddress", message: "Укажите адрес" }, { bad: 1 }] } }, 422, "A");
    await expect(request(response)).rejects.toMatchObject({
      status: 422, code: "validation_error", detail: "Укажите адрес", fields: [{ field: "deliveryAddress", message: "Укажите адрес" }],
    });
  });
});
