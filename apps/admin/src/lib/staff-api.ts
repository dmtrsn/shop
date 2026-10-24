// Browser client for the A4 staff session API (api/admin-openapi.yaml). It is
// the only module that calls it. The HttpOnly staff cookie is the session:
// nothing here reads, stores or forwards a token, and the CSRF token it
// returns is meant for in-memory state only. Customer endpoints, headers and
// cookies are never used.

export const STAFF_SESSION_PATH = "/api/admin/v1/auth/session";

export type Staff = { id: string; email: string; displayName: string };

export type StaffSession =
  | { status: "authenticated"; staff: Staff; csrfToken: string }
  | { status: "anonymous" }
  | { status: "unavailable" };

export type LoginResult =
  | { status: "signed-in"; staff: Staff; csrfToken: string }
  | { status: "invalid" }
  | { status: "rate-limited"; retryAfterSeconds?: number }
  | { status: "unavailable" };

export type LogoutResult =
  // signed-out: Go ended the session; expired: there was none any more.
  | { status: "signed-out" | "expired" }
  // changed: the browser's staff session belongs to someone else now;
  // stale-csrf: the token no longer matches the cookie (a newer sign-in).
  | { status: "changed" | "stale-csrf" | "unavailable" };

type Fetcher = typeof fetch;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Rebuilds the staff identity field by field; anything else is dropped. */
export function parseStaff(value: unknown): Staff | null {
  if (!isRecord(value)) return null;
  const { id, email, displayName } = value;
  if (typeof id !== "string" || !/^stf_[0-9a-f]{32}$/.test(id) || typeof email !== "string" || typeof displayName !== "string") {
    return null;
  }
  return { id, email, displayName };
}

/** Maps a GET session or login body; malformed answers are "unavailable". */
export function parseStaffSession(body: unknown, requireAuthenticatedFlag = true): StaffSession {
  if (!isRecord(body)) return { status: "unavailable" };
  if (requireAuthenticatedFlag) {
    if (typeof body.authenticated !== "boolean") return { status: "unavailable" };
    if (!body.authenticated) return { status: "anonymous" };
  }
  const staff = parseStaff(body.staff);
  if (!staff || typeof body.csrfToken !== "string" || !body.csrfToken.trim()) return { status: "unavailable" };
  return { status: "authenticated", staff, csrfToken: body.csrfToken };
}

async function readJson(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    return undefined;
  }
}

const baseInit: RequestInit = { credentials: "same-origin", cache: "no-store" };

export async function getStaffSession(fetcher: Fetcher = fetch, signal?: AbortSignal): Promise<StaffSession> {
  try {
    const response = await fetcher(STAFF_SESSION_PATH, { ...baseInit, method: "GET", headers: { Accept: "application/json" }, signal });
    if (response.status !== 200) return { status: "unavailable" };
    return parseStaffSession(await readJson(response));
  } catch {
    return { status: "unavailable" };
  }
}

function retryAfter(response: Response): number | undefined {
  const value = Number.parseInt(response.headers.get("Retry-After") ?? "", 10);
  return Number.isFinite(value) && value > 0 ? value : undefined;
}

/**
 * Staff sign-in. Unknown accounts, wrong passwords and disabled accounts are
 * one "invalid" result, as Go answers them alike. 429 comes from the admin
 * edge limiter (plain text) or never; 503 means Go is busy hashing.
 */
export async function loginStaff(email: string, password: string, fetcher: Fetcher = fetch): Promise<LoginResult> {
  let response: Response;
  try {
    response = await fetcher(STAFF_SESSION_PATH, {
      ...baseInit,
      method: "POST",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({ email, password }),
    });
  } catch {
    return { status: "unavailable" };
  }
  switch (response.status) {
    case 200: {
      const session = parseStaffSession(await readJson(response), false);
      return session.status === "authenticated" ? { status: "signed-in", staff: session.staff, csrfToken: session.csrfToken } : { status: "unavailable" };
    }
    case 401:
    case 422:
      return { status: "invalid" };
    case 429: {
      const seconds = retryAfter(response);
      return seconds === undefined ? { status: "rate-limited" } : { status: "rate-limited", retryAfterSeconds: seconds };
    }
    default:
      return { status: "unavailable" };
  }
}

/**
 * Ends the presented staff session. The expected staff ID binds the request
 * to the identity this page shows, so a stale tab never signs out someone
 * else (A4: 409 authenticated_staff_changed).
 */
export async function logoutStaff(csrfToken: string, staffId: string, fetcher: Fetcher = fetch): Promise<LogoutResult> {
  let response: Response;
  try {
    response = await fetcher(STAFF_SESSION_PATH, {
      ...baseInit,
      method: "DELETE",
      headers: { Accept: "application/json", "X-CSRF-Token": csrfToken, "X-Expected-Staff-ID": staffId },
    });
  } catch {
    return { status: "unavailable" };
  }
  switch (response.status) {
    case 204:
      return { status: "signed-out" };
    case 401:
      return { status: "expired" };
    case 403:
      return { status: "stale-csrf" };
    case 409:
      return { status: "changed" };
    default:
      return { status: "unavailable" };
  }
}
