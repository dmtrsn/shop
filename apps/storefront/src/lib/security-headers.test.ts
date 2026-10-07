import { describe, expect, it } from "vitest";

import { nextSecurityHeaders } from "./security-headers";

describe("Next security headers", () => {
  it("preserves the legacy protections while using a nonce-based Next CSP", () => {
    const headers = nextSecurityHeaders("test-nonce");

    expect(headers).toMatchObject({
      "X-Content-Type-Options": "nosniff",
      "X-Frame-Options": "DENY",
      "Referrer-Policy": "strict-origin-when-cross-origin",
      "Permissions-Policy": "camera=(), microphone=(), geolocation=()",
      "Cross-Origin-Opener-Policy": "same-origin",
      "Cross-Origin-Resource-Policy": "same-origin",
    });
    expect(headers["Content-Security-Policy"]).toContain("script-src 'self' 'nonce-test-nonce' 'strict-dynamic'");
    expect(headers["Content-Security-Policy"]).toContain("style-src 'self' 'nonce-test-nonce'");
    expect(headers["Content-Security-Policy"]).not.toContain("'unsafe-inline'");
  });
});
