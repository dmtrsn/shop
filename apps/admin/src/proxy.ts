import { NextResponse, type NextRequest } from "next/server";

import { adminContentSecurityPolicy, adminSecurityHeaders } from "@/lib/security-headers";

export function proxy(request: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
  const requestHeaders = new Headers(request.headers);

  // Next reads this nonce when rendering its framework scripts and inline styles.
  requestHeaders.set("Content-Security-Policy", adminContentSecurityPolicy(nonce));
  requestHeaders.set("x-nonce", nonce);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  for (const [name, value] of Object.entries(adminSecurityHeaders(nonce))) {
    response.headers.set(name, value);
  }
  // Operational route-owner marker used by the admin edge smoke check.
  response.headers.set("X-Admin-Runtime", "next");
  return response;
}

// Hashed build assets keep Next's own immutable caching.
export const config = {
  matcher: ["/((?!_next/static|_next/image).*)"],
};
