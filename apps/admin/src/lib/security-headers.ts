// The storefront baseline (apps/storefront/src/lib/security-headers.ts), made
// stricter for staff pages: no images from data: URLs and no fonts at all
// beyond self, nothing may embed the admin, and responses are never stored or
// indexed. No 'unsafe-inline' or 'unsafe-eval'.
export function adminContentSecurityPolicy(nonce: string): string {
  return [
    "default-src 'self'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
    "object-src 'none'",
    "img-src 'self'",
    `style-src 'self' 'nonce-${nonce}'`,
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`,
    "font-src 'self'",
    "connect-src 'self'",
  ].join("; ");
}

/** Security headers for every page the admin Next runtime serves. */
export function adminSecurityHeaders(nonce: string): Record<string, string> {
  return {
    "Content-Security-Policy": adminContentSecurityPolicy(nonce),
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
    "Referrer-Policy": "no-referrer",
    "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
    "Cross-Origin-Opener-Policy": "same-origin",
    "Cross-Origin-Resource-Policy": "same-origin",
    "X-Robots-Tag": "noindex, nofollow",
    // Staff pages show who is signed in; no cache may keep them.
    "Cache-Control": "no-store",
  };
}
