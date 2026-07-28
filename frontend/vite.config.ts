import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "./",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    // Inline every asset. The release policy forbids CDN or external
    // references in the frontend output, and a separately emitted icon file
    // would be one more request the packaged app has to resolve locally.
    assetsInlineLimit: Number.MAX_SAFE_INTEGER,
    target: "es2022",
  },
  test: {
    include: ["src/**/*.test.{ts,tsx}"],
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    coverage: {
      provider: "v8",
      reporter: ["text", "json-summary"],
      include: ["src/api/**/*.ts", "src/state/**/*.ts", "src/state/**/*.tsx"],
      thresholds: {
        branches: 85,
        functions: 85,
        lines: 85,
        statements: 85,
      },
    },
  },
});
