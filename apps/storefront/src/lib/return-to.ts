// Keep deliberately equivalent to web/migration.js; shared test vectors exercise both.
export function normalizeReturnTo(candidate: unknown, origin: string): string | null {
  if (typeof candidate !== "string" || candidate.length > 2048 || !candidate.startsWith("/") || candidate.startsWith("//")) return null;
  try {
    const decoded = decodeURIComponent(candidate);
    if (/%[0-9a-f]{2}/i.test(decoded)) return null;
    if ([...candidate + decoded].some((char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127 || char === "\\")) return null;
    const base = new URL(origin);
    const url = new URL(candidate, base.origin);
    const decodedURL = new URL(decoded, base.origin);
    if (!["http:", "https:"].includes(base.protocol) || url.origin !== base.origin || decodedURL.origin !== base.origin) return null;
    if (url.username || url.password || decodedURL.username || decodedURL.password) return null;
    if (url.pathname.startsWith("//") || decodedURL.pathname.startsWith("//")) return null;
    // Also reject encoded/dot-normalized entry paths, including a future basePath.
    if (/(^|\/)(legacy|login|auth|api|_next)(\/|$)/i.test(decodedURL.pathname)) return null;
    if (/^#\/?(login|profile)(?:[/?]|$)/i.test(decodedURL.hash)) return null;
    for (const key of url.searchParams.keys()) {
      if (/^(returnTo|token|access_token|csrf|csrfToken|password|authorization|userID|expectedUserID)$/i.test(key)) return null;
    }
    return url.pathname + url.search + url.hash;
  } catch {
    return null;
  }
}

// Fixed placeholder: normalizeReturnTo compares origins, so only same-origin
// relative paths survive. Return targets are app paths without basePath; Next's
// redirect() and Link add it, and the login form restores it before navigating.
const placeholderOrigin = "https://storefront.invalid";

export const loginPath = "/login";

// The only accepted shape is exactly one string value. Anything else, including
// a repeated returnTo, falls back to the storefront home.
export function loginReturnTarget(candidate: unknown): string {
  return typeof candidate === "string" ? normalizeReturnTo(candidate, placeholderOrigin) ?? "/" : "/";
}

export function buildLoginHref(target: string): string {
  return `${loginPath}?${new URLSearchParams({ returnTo: loginReturnTarget(target) })}`;
}
