type RuntimeEnvironment = {
  STOREFRONT_BASE_PATH?: string;
};

/**
 * The Next base path is a build-time setting. Browser API calls deliberately
 * remain rooted at /api so that the edge can keep them same-origin with Go.
 */
export function getStorefrontBasePath(environment: RuntimeEnvironment = process.env as RuntimeEnvironment): string | undefined {
  const value = environment.STOREFRONT_BASE_PATH?.trim();
  if (!value) {
    return undefined;
  }
  if (
    value === "/" ||
    !value.startsWith("/") ||
    value.endsWith("/") ||
    /[?#\s]/.test(value) ||
    value.includes("\\") ||
    value.includes("//")
  ) {
    throw new Error("STOREFRONT_BASE_PATH must be an absolute path without a trailing slash");
  }
  return value;
}
