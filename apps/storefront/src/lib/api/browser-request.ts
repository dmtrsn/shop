import { isRecord } from "./json";

export class BrowserRequestError extends Error {
  constructor(readonly status?: number, readonly code?: string, readonly detail?: string) {
    super(detail || "Browser request failed");
    this.name = "BrowserRequestError";
  }
}

// The response was not bound to the identity that issued the request. Its body
// is never read, so a private payload of another principal cannot be exposed.
export class ResponseOwnerError extends BrowserRequestError {
  constructor(status: number) {
    super(status);
    this.name = "ResponseOwnerError";
  }
}

export type BrowserRequestOptions = { signal?: AbortSignal; timeoutMs?: number; fetcher?: typeof fetch };
export type PrivateRequestOptions = BrowserRequestOptions & { expectedUserID: string };

type RequestInput = BrowserRequestOptions & {
  expectedUserID?: string;
  method?: "GET" | "POST" | "PATCH" | "PUT" | "DELETE";
  csrfToken?: string;
  body?: unknown;
  success?: number;
};

export const authenticatedUserHeader = "X-Authenticated-User-ID";

// Go sets the owner header after authentication and before the protected handler.
// 401 and csrf_failed are rejected earlier and never carry it. A 5xx may come
// from the session lookup itself; it carries no private body and is handled as
// an uncertain failure. Every other response must name the expected owner.
function ownerAccepted(response: Response, expectedUserID: string): boolean {
  if (!expectedUserID) return false;
  if (response.status === 401 || response.status === 403) return true;
  const owner = response.headers.get(authenticatedUserHeader);
  if (response.status >= 500 && owner === null) return true;
  return owner === expectedUserID;
}

function errorDetail(error: Record<string, unknown> | null): string | undefined {
  const fields = error && Array.isArray(error.fields) ? error.fields : [];
  const messages = fields.filter(isRecord)
    .map((field) => typeof field.message === "string" ? field.message : "")
    .filter(Boolean);
  if (messages.length) return messages.join(". ");
  return typeof error?.message === "string" ? error.message : undefined;
}

// Relative same-origin request with the HttpOnly session cookie. Browser code
// never sends Authorization, never stores credentials and never retries.
export async function browserRequest(path: string, options: RequestInput = {}): Promise<unknown> {
  const timeout = AbortSignal.timeout(options.timeoutMs ?? 5_000);
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout;
  const hasBody = options.body !== undefined;
  let response: Response;
  try {
    response = await (options.fetcher ?? fetch)(path, {
      method: options.method ?? "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(options.csrfToken ? { "X-CSRF-Token": options.csrfToken } : {}),
        ...(hasBody ? { "Content-Type": "application/json" } : {}),
      },
      ...(hasBody ? { body: JSON.stringify(options.body) } : {}),
      signal,
    });
  } catch { throw new BrowserRequestError(); }
  if (options.expectedUserID !== undefined && !ownerAccepted(response, options.expectedUserID)) {
    throw new ResponseOwnerError(response.status);
  }
  let body: unknown;
  try { body = await response.json(); } catch { throw new BrowserRequestError(response.status); }
  if (response.status !== (options.success ?? 200)) {
    const error = isRecord(body) && isRecord(body.error) ? body.error : null;
    throw new BrowserRequestError(response.status, typeof error?.code === "string" ? error.code : undefined, errorDetail(error));
  }
  return body;
}
