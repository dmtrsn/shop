import type { NextConfig } from "next";

import { getOptionalAssetRewriteBaseUrl } from "./src/lib/api/config";

const assetRewriteBaseUrl = getOptionalAssetRewriteBaseUrl();

const nextConfig: NextConfig = {
  experimental: {
    // Use the stable TypeScript API; CLI capture is incompatible with this CI filesystem layout.
    useTypeScriptCli: false,
  },
  poweredByHeader: false,
  async rewrites() {
    if (!assetRewriteBaseUrl) return [];
    return [
      {
        source: "/assets/:path*",
        destination: `${assetRewriteBaseUrl}/assets/:path*`,
      },
    ];
  },
};

export default nextConfig;
