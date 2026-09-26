import { getRequiredApiBaseUrl } from "./config";

export type ApiRequestErrorKind =
  | "cancelled"
  | "configuration"
  | "invalid-response"
  | "unavailable";

export class ApiRequestError extends Error {
  readonly kind: ApiRequestErrorKind;
  readonly requestId?: string;
  readonly status?: number;

  constructor(
    kind: ApiRequestErrorKind,
    message: string,
    options: { cause?: unknown; requestId?: string; status?: number } = {},
  ) {
    super(message, { cause: options.cause });
    this.name = "ApiRequestError";
    this.kind = kind;
    this.requestId = options.requestId;
    this.status = options.status;
  }
}

export type ApiRequestOptions = {
  fetcher?: typeof fetch;
  // Sent only when a caller passes them explicitly; nothing from the incoming
  // browser request (cookies, Authorization, Origin) is forwarded implicitly.
  headers?: Readonly<Record<string, string>>;
  signal?: AbortSignal;
  timeoutMs?: number;
};

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function responseRequestId(response: Response, body: unknown): string | undefined {
  const header = response.headers.get("x-request-id") || undefined;
  if (!isRecord(body) || !isRecord(body.error)) return header;
  return typeof body.error.requestId === "string" ? body.error.requestId : header;
}

// Caller headers first, then Accept, so the JSON contract cannot be overridden.
function requestHeaders(extra: ApiRequestOptions["headers"]): Headers {
  const headers = new Headers(extra);
  headers.set("Accept", "application/json");
  return headers;
}

export type ApiJsonResponse = {
  body: unknown;
  requestId?: string;
  status: number;
};

export async function requestApiJsonWithMeta(
  path: string,
  options: ApiRequestOptions = {},
): Promise<ApiJsonResponse> {
  const fetcher = options.fetcher ?? fetch;
  const timeoutSignal = AbortSignal.timeout(options.timeoutMs ?? 5_000);
  const signal = options.signal
    ? AbortSignal.any([options.signal, timeoutSignal])
    : timeoutSignal;

  let baseUrl: string;
  try {
    baseUrl = getRequiredApiBaseUrl();
  } catch (error) {
    throw new ApiRequestError("configuration", "The API service URL is invalid", {
      cause: error,
    });
  }

  let response: Response;
  try {
    response = await fetcher(`${baseUrl}${path}`, {
      cache: "no-store",
      headers: requestHeaders(options.headers),
      signal,
    });
  } catch (error) {
    throw new ApiRequestError(
      options.signal?.aborted ? "cancelled" : "unavailable",
      "The API service could not be reached",
      { cause: error },
    );
  }

  let body: unknown;
  try {
    body = await response.json();
  } catch (error) {
    throw new ApiRequestError("invalid-response", "The API service returned invalid JSON", {
      cause: error,
      requestId: response.headers.get("x-request-id") || undefined,
      status: response.status,
    });
  }

  if (!response.ok) {
    throw new ApiRequestError("unavailable", "The API service returned an error", {
      requestId: responseRequestId(response, body),
      status: response.status,
    });
  }

  return {
    body,
    requestId: responseRequestId(response, body),
    status: response.status,
  };
}

export async function requestApiJson(
  path: string,
  options: ApiRequestOptions = {},
): Promise<unknown> {
  return (await requestApiJsonWithMeta(path, options)).body;
}
