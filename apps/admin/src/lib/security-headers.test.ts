import { describe, expect, it } from "vitest";

import { adminContentSecurityPolicy, adminSecurityHeaders } from "./security-headers";

describe("admin security headers", () => {
  it("use a nonce policy without unsafe sources", () => {
    const policy = adminContentSecurityPolicy("abc");
    expect(policy).toContain("script-src 'self' 'nonce-abc' 'strict-dynamic'");
    expect(policy).toContain("frame-ancestors 'none'");
    expect(policy).toContain("connect-src 'self'");
    expect(policy).not.toMatch(/unsafe-inline|unsafe-eval|\*|https?:/);
  });

  it("forbid caching, framing and indexing", () => {
    const headers = adminSecurityHeaders("abc");
    expect(headers["Cache-Control"]).toBe("no-store");
    expect(headers["X-Frame-Options"]).toBe("DENY");
    expect(headers["X-Robots-Tag"]).toBe("noindex, nofollow");
    expect(headers["Referrer-Policy"]).toBe("no-referrer");
  });
});
