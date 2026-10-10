import {
  getDevelopmentApiProxyBaseUrl,
  getOptionalAssetRewriteBaseUrl,
  type ApiBaseEnvironment,
} from "./config";

type RewriteRule = { source: string; destination: string; basePath?: false };

export type StorefrontRewrites = {
  beforeFiles: RewriteRule[];
  afterFiles: RewriteRule[];
  fallback: RewriteRule[];
};

// The /api proxy is a fallback rewrite: Next checks it only after every
// filesystem route, so route handlers such as /api/search/suggest still win.
export function getStorefrontRewrites(environment: ApiBaseEnvironment = process.env): StorefrontRewrites {
  const assetRewriteBaseUrl = getOptionalAssetRewriteBaseUrl(environment);
  const apiProxyBaseUrl = getDevelopmentApiProxyBaseUrl(environment);
  return {
    // Development has no edge. Mirror legacy's mount and shell resources;
    // production ownership remains exclusively at Nginx.
    beforeFiles: apiProxyBaseUrl ? [
      { source: "/legacy/:path*", destination: apiProxyBaseUrl + "/:path*", basePath: false },
      ...["app.js", "migration.js", "styles.css"].map(file => ({
        source: "/" + file, destination: apiProxyBaseUrl + "/" + file, basePath: false as const,
      })),
    ] : [],
    afterFiles: assetRewriteBaseUrl
      ? [{ source: "/assets/:path*", destination: `${assetRewriteBaseUrl}/assets/:path*` }]
      : [],
    fallback: apiProxyBaseUrl
      ? [{ source: "/api/:path*", destination: `${apiProxyBaseUrl}/api/:path*` }]
      : [],
  };
}
