import { parseFavorites } from "./favorites";
import { isRecord } from "./json";

export type BrowserSession =
  | { status: "authenticated"; userID: string; csrfToken: string }
  | { status: "anonymous" }
  | { status: "unavailable" };

export class FavoritesRequestError extends Error {
  constructor(readonly status?: number, readonly code?: string) {
    super("Favorites request failed");
    this.name = "FavoritesRequestError";
  }
}

type Options = { signal?: AbortSignal; timeoutMs?: number; fetcher?: typeof fetch };

async function request(path: string, options: Options, mutation?: { method: "PUT" | "DELETE"; csrfToken: string }): Promise<unknown> {
  const timeout = AbortSignal.timeout(options.timeoutMs ?? 5_000);
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout;
  let response: Response;
  try {
    response = await (options.fetcher ?? fetch)(path, {
      method: mutation?.method ?? "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(mutation ? { "X-CSRF-Token": mutation.csrfToken } : {}),
      },
      signal,
    });
  } catch { throw new FavoritesRequestError(); }
  let body: unknown;
  try { body = await response.json(); } catch { throw new FavoritesRequestError(response.status); }
  if (response.status !== 200) {
    const code = isRecord(body) && isRecord(body.error) && typeof body.error.code === "string" ? body.error.code : undefined;
    throw new FavoritesRequestError(response.status, code);
  }
  return body;
}

export function parseBrowserSession(body: unknown): BrowserSession {
  if (!isRecord(body) || typeof body.authenticated !== "boolean") return { status: "unavailable" };
  if (!body.authenticated) return { status: "anonymous" };
  if (!isRecord(body.user) || typeof body.user.id !== "string" || !body.user.id ||
      typeof body.csrfToken !== "string" || !body.csrfToken.trim()) return { status: "unavailable" };
  return { status: "authenticated", userID: body.user.id, csrfToken: body.csrfToken };
}

export async function getBrowserSession(options: Options = {}): Promise<BrowserSession> {
  try { return parseBrowserSession(await request("/api/auth/session", options)); }
  catch { return { status: "unavailable" }; }
}

export async function getBrowserFavorites(options: Options = {}) {
  return parseFavorites(await request("/api/favorites", options));
}

export async function setBrowserFavorite(productID: string, enabled: boolean, csrfToken: string, options: Options = {}) {
  return parseFavorites(await request(`/api/favorites/${encodeURIComponent(productID)}`, options, {
    method: enabled ? "PUT" : "DELETE", csrfToken,
  }));
}
