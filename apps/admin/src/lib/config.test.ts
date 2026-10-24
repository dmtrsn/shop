import { describe, expect, it } from "vitest";

import { getAdminApiBaseUrl, getAdminRewrites } from "./config";

describe("admin server configuration", () => {
  it("requires the private Go address in production and validates it", () => {
    expect(() => getAdminApiBaseUrl({ NODE_ENV: "production" })).toThrow(/required/);
    expect(getAdminApiBaseUrl({ NODE_ENV: "production", ADMIN_API_BASE_URL: "http://app:8080/" })).toBe("http://app:8080");
    for (const value of ["app:8080", "ftp://app", "http://app:8080/?x=1", "http://user:pass@app:8080"]) {
      expect(() => getAdminApiBaseUrl({ NODE_ENV: "production", ADMIN_API_BASE_URL: value })).toThrow();
    }
    expect(getAdminApiBaseUrl({ NODE_ENV: "development" })).toBe("http://127.0.0.1:8080");
  });

  it("forwards only the staff API, and only in development", () => {
    expect(getAdminRewrites({ NODE_ENV: "production", ADMIN_API_BASE_URL: "http://app:8080" })).toEqual({ fallback: [] });
    expect(getAdminRewrites({ NODE_ENV: "development" })).toEqual({
      fallback: [{ source: "/api/admin/:path*", destination: "http://127.0.0.1:8080/api/admin/:path*" }],
    });
  });
});
