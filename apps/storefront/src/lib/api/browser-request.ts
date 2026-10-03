import { isRecord } from "./json";

export type FieldError = { field: string; message: string };

export class BrowserRequestError extends Error {
  constructor(
    readonly status?: number,
    readonly code?: string,
    readonly detail?: string,
    readonly fields: readonly FieldError[] = [],
  ) {
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
  // The only caller-supplied header. Credentials, CSRF and identity headers are
  // set here from typed options and can never be overridden by a caller.
  idempotencyKey?: string;
  body?: unknown;
  success?: number;
};

export const authenticatedUserHeader = "X-Authenticated-User-ID";
// Request-side precondition: Go rejects the request before any handler effect
// when the authenticated user is not this one. It is never a credential.
export const expectedUserHeader = "X-Expected-Authenticated-User-ID";

// Go sets the owner header after authentication and before the protected handler.
// 401 and csrf_failed are rejected earlier and never carry it. A 5xx may come
// from the session lookup itself; it carries no private body and is handled as
// an uncertain failure. Every other response must name the expected owner,
// including Go's 409 authenticated_user_changed, which names the actual user.
function ownerAccepted(response: Response, expectedUserID: string): boolean {
  if (!expectedUserID) return false;
  if (response.status === 401 || response.status === 403) return true;
  const owner = response.headers.get(authenticatedUserHeader);
  if (response.status >= 500 && owner === null) return true;
  return owner === expectedUserID;
}

function errorFields(error: Record<string, unknown> | null): FieldError[] {
  const fields = error && Array.isArray(error.fields) ? error.fields : [];
  return fields.filter(isRecord)
    .filter((field) => typeof field.field === "string" && typeof field.message === "string" && field.message)
    .map((field) => ({ field: field.field as string, message: field.message as string }));
}

function errorDetail(error: Record<string, unknown> | null, fields: FieldError[]): string | undefined {
  if (fields.length) return fields.map((field) => field.message).join(". ");
  return typeof error?.message === "string" ? error.message : undefined;
}

// Relative same-origin request with the HttpOnly session cookie. Browser code
// never sends Authorization, never stores credentials and never retries.
export async function browserResponse(path: string, options: RequestInput = {}): Promise<{ body: unknown; headers: Headers }> {
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
        ...(options.idempotencyKey ? { "Idempotency-Key": options.idempotencyKey } : {}),
        ...(options.csrfToken ? { "X-CSRF-Token": options.csrfToken } : {}),
        ...(hasBody ? { "Content-Type": "application/json" } : {}),
        ...(options.expectedUserID ? { [expectedUserHeader]: options.expectedUserID } : {}),
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
    const fields = errorFields(error);
    throw new BrowserRequestError(response.status, typeof error?.code === "string" ? error.code : undefined, errorDetail(error, fields), fields);
  }
  return { body, headers: response.headers };
}

export async function browserRequest(path: string, options: RequestInput = {}): Promise<unknown> {
  return (await browserResponse(path, options)).body;
}
