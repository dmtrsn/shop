import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "@": new URL("./src", import.meta.url).pathname,
      "server-only": new URL("./node_modules/next/dist/compiled/server-only/empty.js", import.meta.url).pathname,
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    // Small worker pool: the development machine is memory-constrained.
    maxWorkers: 2,
  },
});
