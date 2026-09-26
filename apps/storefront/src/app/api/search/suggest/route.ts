import { ApiRequestError, requestApiJsonWithMeta } from "@/lib/api/request";
import {
  codePointLength,
  parseSuggestions,
  SEARCH_QUERY_MAX_LENGTH,
  SUGGESTION_MIN_QUERY_LENGTH,
} from "@/lib/search-suggestions";

// Same path, statuses and envelopes as Go's GET /api/search/suggest, so the browser
// contract does not change when /api/* is later served from one origin.

// Messages mirror writeError and validateSearchQuery in the Go HTTP/application layers.
const VALIDATION_MESSAGE = "Проверьте заполненные поля";
const QUERY_TOO_LONG_MESSAGE = "Поисковый запрос не должен быть длиннее 200 символов";
const INTERNAL_MESSAGE = "Не удалось выполнить запрос";

function json(body: unknown, status: number, requestId: string): Response {
  return Response.json(body, {
    status,
    headers: { "Cache-Control": "no-store", "X-Request-ID": requestId },
  });
}

function validationError(requestId: string): Response {
  return json(
    {
      error: {
        code: "validation_error",
        message: VALIDATION_MESSAGE,
        requestId,
        fields: [{ field: "q", message: QUERY_TOO_LONG_MESSAGE }],
      },
    },
    422,
    requestId,
  );
}

export async function GET(request: Request): Promise<Response> {
  const requestId = request.headers.get("x-request-id") || crypto.randomUUID();
  const query = (new URL(request.url).searchParams.get("q") ?? "").trim();
  const length = codePointLength(query);

  if (length > SEARCH_QUERY_MAX_LENGTH) return validationError(requestId);
  if (length < SUGGESTION_MIN_QUERY_LENGTH) return json({ items: [] }, 200, requestId);

  try {
    const response = await requestApiJsonWithMeta(
      `/api/search/suggest?${new URLSearchParams({ q: query }).toString()}`,
      { signal: request.signal, timeoutMs: 3_000 },
    );
    return json(
      { items: parseSuggestions(response.body) },
      200,
      response.requestId ?? requestId,
    );
  } catch (error) {
    const upstreamRequestId =
      error instanceof ApiRequestError && error.requestId ? error.requestId : requestId;
    // Trimming rules can differ at the edges (for example U+FEFF); keep Go's verdict.
    if (error instanceof ApiRequestError && error.status === 422) {
      return validationError(upstreamRequestId);
    }
    return json(
      { error: { code: "internal_error", message: INTERNAL_MESSAGE, requestId: upstreamRequestId } },
      502,
      upstreamRequestId,
    );
  }
}
