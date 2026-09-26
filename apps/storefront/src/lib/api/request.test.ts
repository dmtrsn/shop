// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiRequestError, requestApiJsonWithMeta } from "./request";

function json(body: unknown, init: ResponseInit = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    ...init,
    headers: { "Content-Type": "application/json", ...init.headers },
  });
}

function sentHeaders(fetcher: ReturnType<typeof vi.fn<typeof fetch>>) {
  return new Headers(fetcher.mock.calls[0][1]?.headers);
}

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("server API transport", () => {
  it("sends only Accept by default, with no-store caching", async () => {
    const fetcher = vi.fn<typeof fetch>(async () => json({ ok: true }));

    await requestApiJsonWithMeta("/api/health", { fetcher });

    expect(fetcher.mock.calls[0][0]).toBe("http://127.0.0.1:8080/api/health");
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store" }));
    expect([...sentHeaders(fetcher).keys()]).toEqual(["accept"]);
    expect(sentHeaders(fetcher).get("accept")).toBe("application/json");
  });

  it("adds explicit caller headers and keeps the JSON Accept header authoritative", async () => {
    const fetcher = vi.fn<typeof fetch>(async () => json({ ok: true }));

    await requestApiJsonWithMeta("/api/auth/session", {
      fetcher,
      headers: { Cookie: "sweetshop_session=abc", Accept: "text/html" },
    });

    const headers = sentHeaders(fetcher);
    expect(headers.get("cookie")).toBe("sweetshop_session=abc");
    expect(headers.get("accept")).toBe("application/json");
    expect(fetcher.mock.calls[0][1]).toEqual(expect.objectContaining({ cache: "no-store" }));
  });

  it("keeps the timeout behavior", async () => {
    const fetcher = vi.fn<typeof fetch>(
      (_input, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(init.signal?.reason));
        }),
    );

    await expect(requestApiJsonWithMeta("/api/health", { fetcher, timeoutMs: 10 })).rejects.toMatchObject({
      kind: "unavailable",
    });
  });

  it("keeps the upstream request ID on invalid JSON", async () => {
    const fetcher = vi.fn<typeof fetch>(
      async () => new Response("<html>", { status: 502, headers: { "X-Request-ID": "go-502" } }),
    );

    const error = await requestApiJsonWithMeta("/api/health", { fetcher }).catch((reason: unknown) => reason);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect(error).toMatchObject({ kind: "invalid-response", requestId: "go-502", status: 502 });
  });
});
