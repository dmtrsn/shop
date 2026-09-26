import {
  getDevelopmentApiProxyBaseUrl,
  getOptionalAssetRewriteBaseUrl,
  type ApiBaseEnvironment,
} from "./config";

type RewriteRule = { source: string; destination: string };

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
    beforeFiles: [],
    afterFiles: assetRewriteBaseUrl
      ? [{ source: "/assets/:path*", destination: `${assetRewriteBaseUrl}/assets/:path*` }]
      : [],
    fallback: apiProxyBaseUrl
      ? [{ source: "/api/:path*", destination: `${apiProxyBaseUrl}/api/:path*` }]
      : [],
  };
}
