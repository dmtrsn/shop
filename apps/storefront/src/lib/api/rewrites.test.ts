import { describe, expect, it } from "vitest";

import { getStorefrontRewrites } from "./rewrites";

describe("storefront rewrites", () => {
  it("proxies /api to Go only as a development fallback, after filesystem routes", () => {
    const rewrites = getStorefrontRewrites({ NODE_ENV: "development" });

    expect(rewrites).toEqual({
      beforeFiles: [
        { source: "/legacy/:path*", destination: "http://127.0.0.1:8080/:path*", basePath: false },
        { source: "/app.js", destination: "http://127.0.0.1:8080/app.js", basePath: false },
        { source: "/migration.js", destination: "http://127.0.0.1:8080/migration.js", basePath: false },
        { source: "/styles.css", destination: "http://127.0.0.1:8080/styles.css", basePath: false },
      ],
      afterFiles: [{ source: "/assets/:path*", destination: "http://127.0.0.1:8080/assets/:path*" }],
      fallback: [{ source: "/api/:path*", destination: "http://127.0.0.1:8080/api/:path*" }],
    });
  });

  it("never lets an /api rewrite run before filesystem routes such as /api/search/suggest", () => {
    for (const environment of [
      { NODE_ENV: "development" },
      { NODE_ENV: "development", STOREFRONT_API_BASE_URL: "http://go.internal:8080" },
      { NODE_ENV: "production", STOREFRONT_API_BASE_URL: "https://api.example.test" },
    ]) {
      const rewrites = getStorefrontRewrites(environment);
      for (const rule of [...rewrites.beforeFiles, ...rewrites.afterFiles]) {
        expect(rule.source.startsWith("/api")).toBe(false);
      }
    }
  });

  it("does not proxy /api in production and keeps the existing asset behavior", () => {
    expect(getStorefrontRewrites({ NODE_ENV: "production" })).toEqual({
      beforeFiles: [],
      afterFiles: [],
      fallback: [],
    });
    expect(
      getStorefrontRewrites({ NODE_ENV: "production", STOREFRONT_API_BASE_URL: "https://api.example.test" }),
    ).toEqual({
      beforeFiles: [],
      afterFiles: [{ source: "/assets/:path*", destination: "https://api.example.test/assets/:path*" }],
      fallback: [],
    });
  });

  it("fails on malformed explicit configuration instead of falling back to localhost", () => {
    expect(() =>
      getStorefrontRewrites({ NODE_ENV: "development", STOREFRONT_API_BASE_URL: "file:///tmp/shop" }),
    ).toThrow("STOREFRONT_API_BASE_URL must use HTTP or HTTPS");
  });
});
