import { defineConfig } from "astro/config";
import sitemap from "@astrojs/sitemap";

const site = process.env.SITE_URL ?? "http://localhost:4321";
const rawBase = process.env.BASE_PATH ?? "/";
const base = rawBase.endsWith("/") ? rawBase : `${rawBase}/`;

export default defineConfig({
  site,
  base,
  output: "static",
  // Chinese stays unprefixed so every published URL keeps working; English is
  // additive under /en/.
  i18n: {
    defaultLocale: "zh-CN",
    locales: ["zh-CN", "en"],
    routing: { prefixDefaultLocale: false },
  },
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
