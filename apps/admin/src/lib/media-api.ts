import { adminRequest, isRecord, type MutationAuth } from "./admin-api";
import { isMediaItem, type MediaItem } from "./products-api";

export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;
export type MediaAsset = MediaItem & { productCount: number; orderCount: number };
export type MediaPage = { items: MediaAsset[]; page: number; totalPages: number };
export type MediaSelection = { primaryId: string | null; galleryIds: string[] };

function parseAsset(body: unknown): MediaAsset | null {
  if (!isRecord(body) || !isMediaItem({ ...body, position: 0 }) || !Number.isSafeInteger(body.productCount) || !Number.isSafeInteger(body.orderCount)) return null;
  return { ...body, position: 0 } as MediaAsset;
}
export function listMedia(page: number, signal?: AbortSignal) {
  return adminRequest(`/api/admin/v1/media?page=${page}&limit=12`, (body): MediaPage | null => {
    if (!isRecord(body) || !Array.isArray(body.items) || !Number.isSafeInteger(body.page) || !Number.isSafeInteger(body.totalPages)) return null;
    const items = body.items.map(parseAsset);
    return items.some((item) => !item) ? null : { items: items as MediaAsset[], page: body.page as number, totalPages: body.totalPages as number };
  }, { signal });
}
export function uploadMedia(file: File, auth: MutationAuth) {
  const body = new FormData();
  body.append("file", file);
  return adminRequest("/api/admin/v1/media", parseAsset, { method: "POST", body, auth });
}
export function deleteMedia(id: string, auth: MutationAuth) {
  return adminRequest(`/api/admin/v1/media/${encodeURIComponent(id)}`, () => true, { method: "DELETE", auth });
}
export function saveProductMedia(id: string, expectedRevision: number, selection: MediaSelection, auth: MutationAuth) {
  return adminRequest(`/api/admin/v1/products/${encodeURIComponent(id)}/media`, (body): number | null =>
    isRecord(body) && body.revision === expectedRevision + 1 ? body.revision as number : null,
  { method: "PUT", body: { expectedRevision, ...selection }, auth });
}
