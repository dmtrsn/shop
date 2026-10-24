const DEFAULT_DEVELOPMENT_API_BASE_URL = "http://127.0.0.1:8080";

export type AdminEnvironment = {
  NODE_ENV?: string;
  ADMIN_API_BASE_URL?: string;
};

/**
 * Private server-to-server Go address used by the admin server to check the
 * staff session (never exposed to the browser and never NEXT_PUBLIC_*).
 * Production requires it; development defaults to a local Go server. It is
 * not the admin origin: the browser always calls /api/admin on its own origin.
 */
export function getAdminApiBaseUrl(environment: AdminEnvironment = process.env): string {
  const value = environment.ADMIN_API_BASE_URL?.trim();
  if (!value) {
    if (environment.NODE_ENV === "production") {
      throw new Error("ADMIN_API_BASE_URL is required when NODE_ENV=production");
    }
    return DEFAULT_DEVELOPMENT_API_BASE_URL;
  }
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("ADMIN_API_BASE_URL must be an absolute HTTP(S) URL");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("ADMIN_API_BASE_URL must use HTTP or HTTPS");
  }
  if (url.search || url.hash || url.username || url.password) {
    throw new Error("ADMIN_API_BASE_URL must not contain credentials, a query or a fragment");
  }
  return url.toString().replace(/\/$/, "");
}

type RewriteRule = { source: string; destination: string };

/**
 * Development has no admin edge, so `next dev` forwards the staff API to Go
 * to keep browser calls same-origin with the staff cookie. Only /api/admin is
 * forwarded: the admin origin never serves the customer API. Production
 * routing belongs to the admin edge alone.
 */
export function getAdminRewrites(environment: AdminEnvironment = process.env): { fallback: RewriteRule[] } {
  if (environment.NODE_ENV !== "development") return { fallback: [] };
  return {
    fallback: [{ source: "/api/admin/:path*", destination: `${getAdminApiBaseUrl(environment)}/api/admin/:path*` }],
  };
}
