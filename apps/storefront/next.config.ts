import type { NextConfig } from "next";

import { getStorefrontRewrites } from "./src/lib/api/rewrites";

const rewrites = getStorefrontRewrites();

const nextConfig: NextConfig = {
  experimental: {
    // Use the stable TypeScript API; CLI capture is incompatible with this CI filesystem layout.
    useTypeScriptCli: false,
  },
  poweredByHeader: false,
  async rewrites() {
    return rewrites;
  },
};

export default nextConfig;
