import type { NextConfig } from "next";

import { getAdminRewrites } from "./src/lib/config";

const rewrites = getAdminRewrites();

const nextConfig: NextConfig = {
  output: "standalone",
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
