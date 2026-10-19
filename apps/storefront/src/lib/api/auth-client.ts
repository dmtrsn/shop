import { BrowserRequestError, browserAnonymousNoContent, browserNoContent, browserRequest, type PrivateRequestOptions } from "./browser-request";
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

export type RegistrationInput = { name: string; email: string; password: string; phone: string };

// Go answers every valid registration and resend with the same 202
// {status:"pending"}, whatever the address is; nothing else is read.
function assertPending(body: unknown): void {
  if (!isRecord(body) || body.status !== "pending") throw new BrowserRequestError(202, "invalid_response");
}

// Starts a self-registration. No session or CSRF token exists yet; Go checks
// Origin. Sent once, never retried. The empty phone is omitted.
export async function registerCustomer(input: RegistrationInput): Promise<void> {
  const body = await browserRequest("/api/auth/registration", {
    method: "POST",
    success: 202,
    body: { name: input.name, email: input.email, password: input.password, ...(input.phone ? { phone: input.phone } : {}) },
  });
  assertPending(body);
}

export async function resendRegistration(email: string): Promise<void> {
  assertPending(await browserRequest("/api/auth/registration/resend", { method: "POST", success: 202, body: { email } }));
}

// Confirms a registration with the emailed token and the chosen password. Like
// startBrowserSession, success only proves the session cookie now exists; the
// body is dropped. The token travels only in this POST body.
export async function confirmRegistration(input: { token: string; password: string }): Promise<void> {
  const body = await browserRequest("/api/auth/registration/confirm", {
    method: "POST",
    body: { token: input.token, password: input.password },
  });
  if (!isRecord(body) || !isRecord(body.user) || typeof body.user.id !== "string" || !body.user.id ||
      typeof body.csrfToken !== "string" || !body.csrfToken.trim()) {
    throw new BrowserRequestError(200, "invalid_response");
  }
}

// Asks for a password-reset email. Go answers every valid address with the
// same 202, whether or not an account exists; nothing else is read.
export async function requestPasswordReset(email: string): Promise<void> {
  assertPending(await browserRequest("/api/auth/password-reset", { method: "POST", success: 202, body: { email } }));
}

// Sets a new password with the emailed reset token. Go answers 204 and signs
// nobody in: every session of the account, and this browser's, is ended.
export function confirmPasswordReset(input: { token: string; password: string }): Promise<void> {
  return browserAnonymousNoContent("/api/auth/password-reset/confirm", {
    method: "POST",
    body: { token: input.token, password: input.password },
  });
}
