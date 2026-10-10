import { headers } from "next/headers";

import type { ApiRequestOptions } from "./request";
import { ApiRequestError, isRecord, requestApiJsonWithMeta } from "./request";

// Go owns the session: it sets these HttpOnly cookies, validates them and
// decides which one is authoritative. The storefront only relays them to Go.
// Secure-mode name first, then the insecure development name.
export const SESSION_COOKIE_NAMES = ["__Host-sweetshop_session", "sweetshop_session"] as const;

export type ServerSessionUser = {
  id: string;
  name: string;
  email: string;
};

export type ServerSession =
  | { status: "authenticated"; user: ServerSessionUser }
  | { status: "anonymous" }
  | { status: "unavailable"; requestId?: string };

// RFC 6265 cookie-octets; anything else is not a cookie Go could have issued and
// must not be spliced into a Cookie header.
const COOKIE_VALUE = /^[\x21\x23-\x2b\x2d-\x3a\x3c-\x5b\x5d-\x7e]+$/;

// Builds a Cookie header from the recognized session cookies only, parsed from
// the raw request header because Next's cookies() keeps one value per name. Every
// occurrence is kept (secure name first, then development name, each in request
// order) so a duplicate that Go treats as ambiguous is never resolved here.
export function sessionCookieHeader(rawCookieHeader: string | null | undefined): string | undefined {
  if (!rawCookieHeader) return undefined;
  const occurrences = new Map<string, string[]>(SESSION_COOKIE_NAMES.map((name) => [name, []]));
  for (const part of rawCookieHeader.split(";")) {
    const separator = part.indexOf("=");
    if (separator < 0) continue;
    occurrences.get(part.slice(0, separator).trim())?.push(part.slice(separator + 1).trim());
  }
  const pairs = SESSION_COOKIE_NAMES.flatMap((name) =>
    occurrences
      .get(name)!
      .filter((value) => COOKIE_VALUE.test(value))
      .map((value) => `${name}=${value}`),
  );
  return pairs.length ? pairs.join("; ") : undefined;
}

function unavailable(requestId?: string): ServerSession {
  return requestId ? { status: "unavailable", requestId } : { status: "unavailable" };
}

// Maps Go's GET /api/auth/session body. The user is rebuilt field by field so
// csrfToken, expiresAt, demoLogin and unknown fields never leave this module.
export function parseServerSession(body: unknown, requestId?: string): ServerSession {
  if (!isRecord(body) || typeof body.authenticated !== "boolean") return unavailable(requestId);
  if (!body.authenticated) return { status: "anonymous" };
  const user = body.user;
  if (
    !isRecord(user) ||
    typeof user.id !== "string" ||
    !user.id ||
    typeof user.name !== "string" ||
    typeof user.email !== "string"
  ) {
    return unavailable(requestId);
  }
  return { status: "authenticated", user: { id: user.id, name: user.name, email: user.email } };
}

/**
 * Reads the Go session for the current request on the server.
 *
 * "anonymous" means Go confirmed there is no session, or the request carries no
 * session cookie. Any failure to get a valid answer from Go is "unavailable",
 * never "anonymous". The result never contains the session token or the CSRF
 * token; browser code that mutates fetches the CSRF token from Go itself.
 */
export async function getServerSession(
  options: Pick<ApiRequestOptions, "fetcher" | "signal" | "timeoutMs"> = {},
): Promise<ServerSession> {
  const cookieHeader = sessionCookieHeader((await headers()).get("cookie"));
  if (!cookieHeader) return { status: "anonymous" };
  try {
    const response = await requestApiJsonWithMeta("/api/auth/session", {
      ...options,
      headers: { Cookie: cookieHeader },
    });
    if (response.status !== 200) return unavailable(response.requestId);
    return parseServerSession(response.body, response.requestId);
  } catch (error) {
    return unavailable(error instanceof ApiRequestError ? error.requestId : undefined);
  }
}

/**
 * Whether Go advertises its seeded demo account. Asks the session endpoint
 * without any browser cookie and returns only the boolean; any failure or an
 * absent/malformed flag is false, so the hint appears only on explicit demo mode.
 */
export async function getDemoLoginAllowed(
  options: Pick<ApiRequestOptions, "fetcher" | "signal" | "timeoutMs"> = {},
): Promise<boolean> {
  try {
    const response = await requestApiJsonWithMeta("/api/auth/session", options);
    return response.status === 200 && isRecord(response.body) && response.body.demoLogin === true;
  } catch {
    return false;
  }
}
