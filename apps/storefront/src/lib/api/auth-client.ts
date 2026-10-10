import { BrowserRequestError, browserNoContent, browserRequest, type PrivateRequestOptions } from "./browser-request";
import { isRecord } from "./json";

// One destructive attempt, never retried by the transport.
export function logoutBrowserSession(options: PrivateRequestOptions & { csrfToken: string }): Promise<void> {
  return browserNoContent("/api/auth/session", { ...options, method: "DELETE" });
}

export type LoginCredentials = { email: string; password: string };

// Go's cookie login: it checks Origin, sets the HttpOnly session cookie and
// answers {user, expiresAt, csrfToken}. Success only proves that the session
// exists. Nothing from the body is returned, so the CSRF token cannot be kept;
// the next page reads its own from GET /api/auth/session. Sent once, never retried.
export async function startBrowserSession(credentials: LoginCredentials): Promise<void> {
  const body = await browserRequest("/api/auth/session", {
    method: "POST",
    body: { email: credentials.email, password: credentials.password },
  });
  if (!isRecord(body) || !isRecord(body.user) || typeof body.user.id !== "string" || !body.user.id ||
      typeof body.csrfToken !== "string" || !body.csrfToken.trim()) {
    throw new BrowserRequestError(200, "invalid_response");
  }
}
