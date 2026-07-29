import type { APIRoute } from "astro";

export const prerender = true;

export const GET: APIRoute = ({ site }) => {
  const base = new URL(import.meta.env.BASE_URL, site ?? "http://localhost:4321");
  const sitemap = new URL("sitemap-index.xml", base);
  return new Response(`User-agent: *\nAllow: ${base.pathname}\nSitemap: ${sitemap}\n`, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
