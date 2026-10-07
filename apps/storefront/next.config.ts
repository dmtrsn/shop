import type { NextConfig } from "next";

import { getStorefrontRewrites } from "./src/lib/api/rewrites";
import { getStorefrontBasePath } from "./src/lib/runtime-config";

const rewrites = getStorefrontRewrites();
const basePath = getStorefrontBasePath();

const nextConfig: NextConfig = {
  output: "standalone",
  basePath,
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
