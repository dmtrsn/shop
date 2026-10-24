import "server-only";

import { headers } from "next/headers";

import { getAdminApiBaseUrl } from "./config";
import { parseStaffSession, STAFF_SESSION_PATH, type Staff } from "./staff-api";

// Go owns the staff session: it sets this HttpOnly cookie, validates it and
// decides which value counts. The admin server only relays it. Secure-mode
// name first, then the development name. The customer cookie is never sent.
export const STAFF_COOKIE_NAMES = ["__Host-sweetshop_staff", "sweetshop_staff"] as const;

// Sanitized route gate: never the CSRF token, the cookie or anything else Go
// returned, so nothing secret reaches server-rendered HTML.
export type ServerStaffSession =
  | { status: "authenticated"; staff: Staff }
  | { status: "anonymous" }
  | { status: "unavailable" };

// RFC 6265 cookie-octets; anything else is not a cookie Go could have issued.
const COOKIE_VALUE = /^[\x21\x23-\x2b\x2d-\x3a\x3c-\x5b\x5d-\x7e]+$/;

/**
 * The Cookie header to relay: staff cookies only, every occurrence in request
 * order, so a duplicate Go treats as ambiguous is never resolved here.
 */
export function staffCookieHeader(rawCookieHeader: string | null | undefined): string | undefined {
  if (!rawCookieHeader) return undefined;
  const occurrences = new Map<string, string[]>(STAFF_COOKIE_NAMES.map((name) => [name, []]));
  for (const part of rawCookieHeader.split(";")) {
    const separator = part.indexOf("=");
    if (separator < 0) continue;
    occurrences.get(part.slice(0, separator).trim())?.push(part.slice(separator + 1).trim());
  }
  const pairs = STAFF_COOKIE_NAMES.flatMap((name) =>
    occurrences.get(name)!.filter((value) => COOKIE_VALUE.test(value)).map((value) => `${name}=${value}`),
  );
  return pairs.length ? pairs.join("; ") : undefined;
}

type CheckOptions = { fetcher?: typeof fetch; baseUrl?: string; timeoutMs?: number };

/**
 * Asks Go who the presented staff cookie belongs to. "anonymous" means Go
 * said so, or there is no staff cookie; any other outcome, including a slow
 * or broken backend, is "unavailable" and never "anonymous".
 */
export async function checkStaffSession(rawCookieHeader: string | null | undefined, options: CheckOptions = {}): Promise<ServerStaffSession> {
  const cookie = staffCookieHeader(rawCookieHeader);
  if (!cookie) return { status: "anonymous" };
  const fetcher = options.fetcher ?? fetch;
  try {
    const response = await fetcher(`${options.baseUrl ?? getAdminApiBaseUrl()}${STAFF_SESSION_PATH}`, {
      method: "GET",
      headers: { Accept: "application/json", Cookie: cookie },
      cache: "no-store",
      redirect: "manual",
      signal: AbortSignal.timeout(options.timeoutMs ?? 5000),
    });
    if (response.status !== 200) return { status: "unavailable" };
    const session = parseStaffSession(await response.json());
    if (session.status === "authenticated") return { status: "authenticated", staff: session.staff };
    return session;
  } catch {
    return { status: "unavailable" };
  }
}

/** The staff session of the request being rendered. */
export async function getServerStaffSession(): Promise<ServerStaffSession> {
  return checkStaffSession((await headers()).get("cookie"));
}
