import { describe, expect, it, vi } from "vitest";
import { favoriteProduct, jsonResponse } from "@/test/favorites-fixtures";
import { repeatLine, repeatPreview, repeatResult } from "@/test/repeat-fixtures";
import { parseRepeatPreview, parseRepeatReceipt, parseRepeatResult } from "./repeat";
import { commitBrowserRepeat, previewBrowserRepeat } from "./repeat-client";
import { repeatBody, repeatDrafts } from "../repeat-review";
import { BrowserRequestError, ResponseOwnerError } from "./browser-request";

describe("strict v2 repeat boundary", () => {
  it("keeps duplicate products by stored positions, signed positions/deltas, and truthful missing originals", () => {
    const preview = repeatPreview(repeatLine(-4), repeatLine(5, "p01", { status: "limited", priceDeltaMinor: -100 }),
      repeatLine(8, "gone", { status: "discontinued", product: undefined, replacement: favoriteProduct("replacement") }),
      repeatLine(9, "p02", { status: "unavailable", suggestedQuantity: 0, proposedQuantity: 0 }));
    expect(parseRepeatPreview(preview)).toEqual(preview);
    expect(parseRepeatPreview({ ...preview, items: [{ ...preview.items[2], product: null }] }).items[0].product).toBeUndefined();
  });
  it.each(["sourcePosition", "requestedQuantity", "originalUnitPriceMinor", "existingCartQuantity", "suggestedQuantity", "proposedQuantity", "priceDeltaMinor"])("rejects unsafe %s", (field) => {
    expect(() => parseRepeatPreview(repeatPreview({ ...repeatLine(), [field]: Number.MAX_SAFE_INTEGER + 1 }))).toThrow();
  });
  it.each([{}, { items: null }, { items: [{}] }])("rejects malformed previews", (value) => expect(() => parseRepeatPreview(value)).toThrow());
  it("rejects duplicate source identities, wrong current original and impossible status", () => {
    expect(() => parseRepeatPreview(repeatPreview(repeatLine(), repeatLine()))).toThrow();
    expect(() => parseRepeatPreview(repeatPreview(repeatLine(1, "p01", { product: favoriteProduct("other") })))).toThrow();
    expect(() => parseRepeatPreview(repeatPreview(repeatLine(1, "p01", { product: undefined })))).toThrow();
  });
  it("parses receipt separately and uses the existing full Cart parser", () => {
    const result = repeatResult();
    expect(parseRepeatReceipt(result.receipt)).toEqual(result.receipt);
    expect(parseRepeatResult(result)).toEqual(result);
    expect(() => parseRepeatResult({ ...result, cart: {} })).toThrow();
    expect(() => parseRepeatReceipt({ ...result.receipt, addedCount: 2 })).toThrow();
    expect(() => parseRepeatReceipt({ ...result.receipt, addedQuantity: Number.MAX_SAFE_INTEGER + 1 })).toThrow();
  });
  it("POSTs only v2 with owner, CSRF, credentials; preview has no key or Cart read", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse(repeatPreview()));
    await previewBrowserRepeat("#10482", "csrf", { expectedUserID: "A", fetcher });
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher.mock.calls[0]).toMatchObject(["/api/v2/orders/%2310482/repeat/preview", { method: "POST", credentials: "same-origin", headers: { "X-CSRF-Token": "csrf", "X-Expected-Authenticated-User-ID": "A" } }]);
    expect(fetcher.mock.calls[0][1]?.headers).not.toHaveProperty("Idempotency-Key");
  });
  it("sends exact reviewed body/key and parses replay; rejects empty selections before any request", async () => {
    const response = jsonResponse(repeatResult()); response.headers.set("Idempotent-Replayed", "true");
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response);
    const preview = repeatPreview(); const body = repeatBody(preview, repeatDrafts(preview));
    const result = await commitBrowserRepeat("ord-1", body, "a-valid-repeat-key", "csrf", { expectedUserID: "A", fetcher });
    expect(result.replayed).toBe(true);
    expect(fetcher.mock.calls[0][0]).toBe("/api/v2/orders/ord-1/repeat/commit");
    expect(JSON.parse(fetcher.mock.calls[0][1]?.body as string)).toEqual(body);
    expect(fetcher.mock.calls[0][1]?.headers).toMatchObject({ "Idempotency-Key": "a-valid-repeat-key" });
    await expect(commitBrowserRepeat("ord-1", { items: [] }, "a-valid-repeat-key", "csrf", { expectedUserID: "A", fetcher })).rejects.toThrow();
    await expect(commitBrowserRepeat("ord-1", body, "", "csrf", { expectedUserID: "A", fetcher })).rejects.toThrow();
    expect(fetcher).toHaveBeenCalledOnce();
  });
  it.each([401, 403, 404, 409, 422, 500])("preserves error mapping %s", async (status) => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(jsonResponse({ error: { code: "test", fields: [{ field: "items[0].quantity", message: "Bad quantity" }] } }, status));
    await expect(previewBrowserRepeat("ord-1", "csrf", { expectedUserID: "A", fetcher })).rejects.toMatchObject({ status, code: "test", fields: [{ field: "items[0].quantity", message: "Bad quantity" }] });
  });
  it("rejects foreign responses without accepting their body and maps timeouts", async () => {
    await expect(previewBrowserRepeat("ord-1", "csrf", { expectedUserID: "A", fetcher: vi.fn().mockResolvedValue(jsonResponse(repeatPreview(), 200, "B")) })).rejects.toBeInstanceOf(ResponseOwnerError);
    await expect(previewBrowserRepeat("ord-1", "csrf", { expectedUserID: "A", fetcher: vi.fn().mockRejectedValue(new DOMException("Timeout", "TimeoutError")) })).rejects.toBeInstanceOf(BrowserRequestError);
  });
});
