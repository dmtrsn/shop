// The admin request layer: one place that talks to /api/admin/v1 for data and
// turns every answer into a typed result. Go's error codes become a small set
// of failures; raw server text never reaches the page. Session endpoints stay
// in staff-api.ts.

export type FieldError = { field: string; message: string };

export type ApiFailure =
  | { kind: "validation"; fields: FieldError[] }
  | { kind: "stale_revision" | "not_found" | "conflict" | "unauthorized" | "csrf" | "staff_changed" | "unavailable" }
  | { kind: "failed"; requestId?: string };

export type ApiResult<T> = { ok: true; data: T } | { ok: false; error: ApiFailure };

/** Credentials of one unsafe staff request: never stored, only sent. */
export type MutationAuth = { csrfToken: string; staffId: string };

export const failureMessages: Record<ApiFailure["kind"], string> = {
  validation: "Проверьте выделенные поля.",
  stale_revision: "Товар был изменён другим сотрудником. Ваши изменения не сохранены.",
  not_found: "Товар не найден.",
  conflict: "Действие нельзя выполнить в текущем состоянии товара. Загрузите актуальную версию.",
  unauthorized: "Сеанс завершён. Войдите снова.",
  csrf: "Не удалось подтвердить запрос. Обновите страницу и повторите.",
  staff_changed: "В этом браузере выполнен вход под другой учётной записью сотрудника. Обновите страницу.",
  unavailable: "Сервис временно недоступен. Повторите попытку.",
  failed: "Не удалось выполнить запрос. Повторите попытку.",
};

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function readJson(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    return undefined;
  }
}

function failure(status: number, body: unknown): ApiFailure {
  const error = isRecord(body) && isRecord(body.error) ? body.error : {};
  const fields = Array.isArray(error.fields)
    ? error.fields.filter(
        (field): field is FieldError => isRecord(field) && typeof field.field === "string" && typeof field.message === "string",
      )
    : [];
  switch (error.code) {
    case "validation_error":
      return { kind: "validation", fields: fields.map(({ field, message }) => ({ field, message })) };
    case "stale_revision":
      return { kind: "stale_revision" };
    case "not_found":
      return { kind: "not_found" };
    case "conflict":
      return { kind: "conflict" };
    case "unauthorized":
      return { kind: "unauthorized" };
    case "csrf_failed":
      return { kind: "csrf" };
    case "authenticated_staff_changed":
      return { kind: "staff_changed" };
  }
  if (status === 401) return { kind: "unauthorized" };
  if (status === 429 || status === 502 || status === 503 || status === 504) return { kind: "unavailable" };
  return typeof error.requestId === "string" ? { kind: "failed", requestId: error.requestId } : { kind: "failed" };
}

export type RequestOptions = {
  method?: "GET" | "POST" | "PUT";
  body?: unknown;
  auth?: MutationAuth;
  signal?: AbortSignal;
  fetcher?: typeof fetch;
};

/**
 * Sends one request and validates the success body with parse. Unsafe
 * requests carry the staff CSRF token and the expected staff ID; nothing
 * customer-specific is ever sent.
 */
export async function adminRequest<T>(path: string, parse: (body: unknown) => T | null, options: RequestOptions = {}): Promise<ApiResult<T>> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (options.auth) {
    headers["X-CSRF-Token"] = options.auth.csrfToken;
    headers["X-Expected-Staff-ID"] = options.auth.staffId;
  }
  let response: Response;
  try {
    response = await (options.fetcher ?? fetch)(path, {
      method: options.method ?? "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    });
  } catch {
    return { ok: false, error: { kind: "unavailable" } };
  }
  const body = await readJson(response);
  if (!response.ok) return { ok: false, error: failure(response.status, body) };
  const data = parse(body);
  return data === null ? { ok: false, error: { kind: "failed" } } : { ok: true, data };
}
