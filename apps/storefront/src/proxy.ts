import { NextResponse, type NextRequest } from "next/server";

import { nextContentSecurityPolicy, nextSecurityHeaders } from "@/lib/security-headers";

export function proxy(request: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
  const contentSecurityPolicy = nextContentSecurityPolicy(nonce);
  const requestHeaders = new Headers(request.headers);

  // Next reads this nonce when rendering its framework scripts and inline styles.
  requestHeaders.set("Content-Security-Policy", contentSecurityPolicy);
  requestHeaders.set("x-nonce", nonce);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  for (const [name, value] of Object.entries(nextSecurityHeaders(nonce))) {
    response.headers.set(name, value);
  }
  // This is an operational route-owner marker used by the edge smoke check.
  response.headers.set("X-Storefront-Runtime", "next");
  return response;
}
