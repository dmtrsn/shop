const sharedSecurityHeaders = {
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
  "Cross-Origin-Opener-Policy": "same-origin",
  "Cross-Origin-Resource-Policy": "same-origin",
} as const;

export function nextContentSecurityPolicy(nonce: string): string {
  return [
    "default-src 'self'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
    "object-src 'none'",
    "img-src 'self' data:",
    `style-src 'self' 'nonce-${nonce}'`,
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`,
    "font-src 'self' data:",
    "connect-src 'self'",
  ].join("; ");
}

/** Security-header owner for every response served by the Next runtime. */
export function nextSecurityHeaders(nonce: string): Record<string, string> {
  return {
    ...sharedSecurityHeaders,
    "Content-Security-Policy": nextContentSecurityPolicy(nonce),
  };
}
