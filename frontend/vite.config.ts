import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig(({ command }) => ({
  // Relative base is required for the packaged app (assets served from an
  // embedded FS with no fixed root), but Vite's dev server does not support
  // a relative base and will emit broken asset URLs (blank screen under
  // `wails3 dev`). Only apply it to the production build.
  base: command === "build" ? "./" : "/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: false,
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
      // The maintained binding adapter and UI state stay covered; browser E2E
      // moves to the Wails server-build phase.
      include: [
        "src/backend/**/*.ts",
        "src/state/**/*.ts",
        "src/state/**/*.tsx",
      ],
      thresholds: {
        branches: 85,
        functions: 85,
        lines: 85,
        statements: 85,
      },
    },
  },
}));
