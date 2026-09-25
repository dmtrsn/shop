import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  experimental: {
    // Use the stable TypeScript API; CLI capture is incompatible with this CI filesystem layout.
    useTypeScriptCli: false,
  },
  poweredByHeader: false,
};

export default nextConfig;
