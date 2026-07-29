import type { APIRoute } from "astro";
import releaseIndex from "../generated/release-index.json";

export const prerender = true;

export const GET: APIRoute = () =>
  new Response(`${JSON.stringify(releaseIndex, null, 2)}\n`, {
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Cache-Control": "public, max-age=300",
      "X-Content-Type-Options": "nosniff",
    },
  });
