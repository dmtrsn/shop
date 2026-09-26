const DEFAULT_API_BASE_URL = "http://127.0.0.1:8080";

export type ApiBaseEnvironment = {
  NODE_ENV?: string;
  STOREFRONT_API_BASE_URL?: string;
};

function configuredApiBaseUrl(environment: ApiBaseEnvironment): string | undefined {
  const configuredValue = environment.STOREFRONT_API_BASE_URL?.trim();
  if (!configuredValue) return undefined;

  let url: URL;
  try {
    url = new URL(configuredValue);
  } catch {
    throw new Error("STOREFRONT_API_BASE_URL must be an absolute HTTP(S) URL");
  }

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("STOREFRONT_API_BASE_URL must use HTTP or HTTPS");
  }
  if (url.search || url.hash) {
    throw new Error("STOREFRONT_API_BASE_URL must not contain a query or fragment");
  }

  return url.toString().replace(/\/$/, "");
}

export function getRequiredApiBaseUrl(
  environment: ApiBaseEnvironment = process.env,
): string {
  const configuredValue = configuredApiBaseUrl(environment);
  if (configuredValue) return configuredValue;
  if (environment.NODE_ENV === "production") {
    throw new Error("STOREFRONT_API_BASE_URL is required when NODE_ENV=production");
  }
  return DEFAULT_API_BASE_URL;
}

export function getOptionalAssetRewriteBaseUrl(
  environment: ApiBaseEnvironment = process.env,
): string | undefined {
  const configuredValue = configuredApiBaseUrl(environment);
  if (configuredValue) return configuredValue;
  return environment.NODE_ENV === "development" ? DEFAULT_API_BASE_URL : undefined;
}

// Development-only target for proxying browser /api/* calls to Go through the
// Next dev server, so browser requests stay same-origin with the session cookie.
// Production never proxies: the public edge routes /api/* to Go directly.
export function getDevelopmentApiProxyBaseUrl(
  environment: ApiBaseEnvironment = process.env,
): string | undefined {
  if (environment.NODE_ENV !== "development") return undefined;
  return getRequiredApiBaseUrl(environment);
}
