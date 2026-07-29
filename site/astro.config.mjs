import { defineConfig } from "astro/config";
import sitemap from "@astrojs/sitemap";

const site = process.env.SITE_URL ?? "http://localhost:4321";
const rawBase = process.env.BASE_PATH ?? "/";
const base = rawBase.endsWith("/") ? rawBase : `${rawBase}/`;

export default defineConfig({
  site,
  base,
  output: "static",
  integrations: [sitemap()],
  build: {
    assets: "_assets",
  },
  vite: {
    build: {
      sourcemap: false,
    },
  },
});
