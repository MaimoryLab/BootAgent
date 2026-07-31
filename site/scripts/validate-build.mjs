import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("../dist/", import.meta.url);
const rootPath = root.pathname;
const failures = [];
const htmlFiles = [];
const configuredBase = (process.env.BASE_PATH ?? "/").replace(/^\/+|\/+$/g, "");
const configuredBasePrefix = configuredBase ? `/${configuredBase}/` : "/";

function walk(directory) {
  for (const name of readdirSync(directory)) {
    const path = join(directory, name);
    if (statSync(path).isDirectory()) walk(path);
    else if (name.endsWith(".html")) htmlFiles.push(path);
    else if (name.endsWith(".map")) failures.push(`Source map is forbidden: ${relative(rootPath, path)}`);
  }
}

function localTarget(href) {
  const clean = href.split("#", 1)[0].split("?", 1)[0];
  if (!clean || clean.startsWith("mailto:") || clean.startsWith("http://") || clean.startsWith("https://") || clean.startsWith("data:")) return null;
  let normalized = clean.startsWith("/") ? clean.slice(1) : clean;
  if (configuredBase && (normalized === configuredBase || normalized.startsWith(`${configuredBase}/`))) {
    normalized = normalized.slice(configuredBase.length).replace(/^\//, "");
  }
  if (!normalized) return join(rootPath, "index.html");
  const direct = join(rootPath, normalized);
  if (existsSync(direct) && statSync(direct).isFile()) return direct;
  if (existsSync(direct) && statSync(direct).isDirectory()) return join(direct, "index.html");
  if (!normalized.includes(".")) return join(rootPath, normalized, "index.html");
  return direct;
}

walk(rootPath);
for (const required of ["index.html", "downloads/index.html", "quickstart/index.html", "agents/index.html", "providers/index.html", "security/index.html"]) {
  if (!existsSync(join(rootPath, required))) failures.push(`Missing required output: ${required}`);
}

for (const path of htmlFiles) {
  const text = readFileSync(path, "utf8");
  const label = relative(rootPath, path);
  for (const required of ["<title>", 'name="description"', 'rel="canonical"', "<h1"]) {
    if (!text.includes(required)) failures.push(`${label} is missing ${required}`);
  }
  const baseHref = text.match(/<base href="([^"]+)"/)?.[1];
  if (!baseHref) failures.push(`${label} is missing a base URL`);
  else if (new URL(baseHref).pathname !== configuredBasePrefix) failures.push(`${label} has the wrong base URL: ${baseHref}`);
  const socialImage = text.match(/<meta property="og:image" content="([^"]+)"/)?.[1];
  if (!socialImage) failures.push(`${label} is missing an Open Graph image`);
  else if (!new URL(socialImage).pathname.startsWith(`${configuredBasePrefix}images/`)) failures.push(`${label} has an Open Graph image outside the base path: ${socialImage}`);
  if (/<script[^>]+src=["']https?:\/\//i.test(text) || /<img[^>]+src=["']https?:\/\//i.test(text) || /<link[^>]+rel=["']stylesheet["'][^>]+href=["']https?:\/\//i.test(text)) failures.push(`${label} loads a remote script, image, or stylesheet`);
  if (/sk-[A-Za-z0-9_-]{20,}/.test(text)) failures.push(`${label} appears to contain an API key`);
  const matches = text.matchAll(/(?:href|src)="([^"]+)"/g);
  for (const [, href] of matches) {
    if (configuredBase && href.startsWith("/") && !href.startsWith(configuredBasePrefix) && !href.startsWith("//")) {
      failures.push(`${label} escapes the configured base path: ${href}`);
    }
    const target = localTarget(href);
    if (target && !existsSync(target)) failures.push(`${label} has a broken local reference: ${href}`);
  }
}

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log(`Validated ${htmlFiles.length} HTML pages.`);
