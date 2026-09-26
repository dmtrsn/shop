import { describe, expect, it } from "vitest";

import {
  getDevelopmentApiProxyBaseUrl,
  getOptionalAssetRewriteBaseUrl,
  getRequiredApiBaseUrl,
} from "./config";

describe("API base configuration", () => {
  it("validates and normalizes an explicitly configured value", () => {
    const environment = {
      NODE_ENV: "production",
      STOREFRONT_API_BASE_URL: " https://api.example.test/shop/ ",
    };

    expect(getRequiredApiBaseUrl(environment)).toBe("https://api.example.test/shop");
    expect(getOptionalAssetRewriteBaseUrl(environment)).toBe("https://api.example.test/shop");
  });

  it("uses the localhost fallback for development", () => {
    const environment = { NODE_ENV: "development" };

    expect(getRequiredApiBaseUrl(environment)).toBe("http://127.0.0.1:8080");
    expect(getOptionalAssetRewriteBaseUrl(environment)).toBe("http://127.0.0.1:8080");
  });

  it("requires an explicit API base for production requests", () => {
    expect(() => getRequiredApiBaseUrl({ NODE_ENV: "production" })).toThrow(
      "STOREFRONT_API_BASE_URL is required when NODE_ENV=production",
    );
  });

  it("omits the asset rewrite base in production when configuration is absent", () => {
    expect(getOptionalAssetRewriteBaseUrl({ NODE_ENV: "production" })).toBeUndefined();
  });

  it("rejects an invalid explicitly configured value in every environment", () => {
    const environment = {
      NODE_ENV: "development",
      STOREFRONT_API_BASE_URL: "file:///tmp/shop",
    };

    expect(() => getRequiredApiBaseUrl(environment)).toThrow(
      "STOREFRONT_API_BASE_URL must use HTTP or HTTPS",
    );
    expect(() => getOptionalAssetRewriteBaseUrl(environment)).toThrow(
      "STOREFRONT_API_BASE_URL must use HTTP or HTTPS",
    );
  });

  it("derives the development API proxy target only in development", () => {
    expect(getDevelopmentApiProxyBaseUrl({ NODE_ENV: "development" })).toBe("http://127.0.0.1:8080");
    expect(
      getDevelopmentApiProxyBaseUrl({
        NODE_ENV: "development",
        STOREFRONT_API_BASE_URL: "http://go.internal:8080/",
      }),
    ).toBe("http://go.internal:8080");
    for (const NODE_ENV of ["production", "test", undefined]) {
      expect(getDevelopmentApiProxyBaseUrl({ NODE_ENV })).toBeUndefined();
      expect(
        getDevelopmentApiProxyBaseUrl({ NODE_ENV, STOREFRONT_API_BASE_URL: "https://api.example.test" }),
      ).toBeUndefined();
    }
  });

  it("rejects malformed configuration for the development proxy like other consumers", () => {
    for (const STOREFRONT_API_BASE_URL of ["not a url", "ftp://go.internal", "http://go.internal/?q=1"]) {
      expect(() =>
        getDevelopmentApiProxyBaseUrl({ NODE_ENV: "development", STOREFRONT_API_BASE_URL }),
      ).toThrow(/STOREFRONT_API_BASE_URL/);
    }
  });
});
