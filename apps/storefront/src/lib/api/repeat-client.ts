import { browserRequest, browserResponse, type PrivateRequestOptions } from "./browser-request";
import { parseRepeatPreview, parseRepeatResult, type RepeatBody, type RepeatResult } from "./repeat";

const path = (orderID: string, action: "preview" | "commit") => `/api/v2/orders/${encodeURIComponent(orderID)}/repeat/${action}`;
export async function previewBrowserRepeat(orderID: string, csrfToken: string, options: PrivateRequestOptions) {
  return parseRepeatPreview(await browserRequest(path(orderID, "preview"), { ...options, method: "POST", csrfToken }));
}
export async function commitBrowserRepeat(orderID: string, body: RepeatBody, key: string, csrfToken: string, options: PrivateRequestOptions): Promise<RepeatResult> {
  if (!/^[A-Za-z0-9_-]{16,128}$/.test(key) || !body.items?.length) throw new Error("Repeat requires an explicit selection and key");
  const response = await browserResponse(path(orderID, "commit"), {
    timeoutMs: 20_000, ...options, method: "POST", body, csrfToken, idempotencyKey: key,
  });
  const result = parseRepeatResult(response.body);
  // A valid private response must also describe the exact logical operation.
  const additions = [...body.items].sort((a, b) => a.sourcePosition - b.sourcePosition);
  if (result.receipt.sourceOrderId !== orderID || result.receipt.additions.length !== additions.length ||
      result.receipt.additions.some((row, i) => row.sourcePosition !== additions[i].sourcePosition ||
        row.productId !== additions[i].productId || row.quantity !== additions[i].quantity)) throw new Error("Unexpected repeat receipt");
  return { ...result, replayed: response.headers.get("Idempotent-Replayed") === "true" };
}
