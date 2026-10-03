import { parseFavorites } from "./favorites";
import { browserRequest, type PrivateRequestOptions } from "./browser-request";
export { BrowserRequestError as FavoritesRequestError } from "./browser-request";
export { getBrowserSession, parseBrowserSession, type BrowserSession } from "./browser-session";
export async function getBrowserFavorites(options: PrivateRequestOptions) {
  return parseFavorites(await browserRequest("/api/favorites", options));
}
export async function setBrowserFavorite(productID: string, enabled: boolean, csrfToken: string, options: PrivateRequestOptions) {
  return parseFavorites(await browserRequest(`/api/favorites/${encodeURIComponent(productID)}`, {
    ...options, method: enabled ? "PUT" : "DELETE", csrfToken,
  }));
}
