import "server-only";

type LoginEnvironment = {
  NODE_ENV?: string;
  STOREFRONT_LEGACY_LOGIN_URL?: string;
};

// Only this resolved navigation href may cross the server/client boundary.
// The API origin is deliberately not an input to this configuration.
export function getLegacyLoginHref(environment: LoginEnvironment = process.env): string | null {
  const configured = environment.STOREFRONT_LEGACY_LOGIN_URL?.trim();
  if (!configured) {
    return environment.NODE_ENV === "development" ? "http://127.0.0.1:8080/#/login" : null;
  }
  const invalid = () => new Error("STOREFRONT_LEGACY_LOGIN_URL must be an absolute HTTP(S) navigation URL without credentials");
  // Reject browser-normalized backslashes/control characters and ambiguous URLs.
  if (!/^https?:\/\//i.test(configured) || [...configured].some((char) => char.charCodeAt(0) <= 32 || char.charCodeAt(0) === 127 || char === "\\")) throw invalid();
  let url: URL;
  try { url = new URL(configured); } catch { throw invalid(); }
  if (!url.hostname || url.username || url.password || !["http:", "https:"].includes(url.protocol)) throw invalid();
  return url.href;
}
