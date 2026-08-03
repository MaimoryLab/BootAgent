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
// The /en/ entries are listed for the same reason as the rest: a translated page
// silently dropping out of the build is otherwise invisible, since the link
// checker below only sees links that were actually emitted.
//
// release-index.json is no longer among them: the site reads release data from
// GitHub Releases at build time instead of republishing a locally generated
// index, so there is no such artifact to require.
for (const required of ["index.html", "downloads/index.html", "quickstart/index.html", "agents/index.html", "providers/index.html", "security/index.html", "explore/index.html", "en/index.html", "en/downloads/index.html", "en/quickstart/index.html", "en/explore/index.html", "en/security/index.html", "llms.txt", "site.webmanifest"]) {
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
  // A page's declared language has to match the directory it was emitted into,
  // and any hreflang set has to be reciprocal and carry an x-default. Neither is
  // visible to the link check below, which only sees hrefs.
  const declaredLang = text.match(/<html lang="([^"]+)"/)?.[1];
  const expectedLang = label.startsWith("en/") || label === "en.html" ? "en" : "zh-CN";
  if (!declaredLang) failures.push(`${label} is missing a lang attribute`);
  else if (declaredLang !== expectedLang) failures.push(`${label} declares lang="${declaredLang}" but sits under ${expectedLang}`);
  const alternates = [...text.matchAll(/<link rel="alternate" hreflang="([^"]+)"/g)].map(([, code]) => code);
  if (alternates.length && !alternates.includes("x-default")) {
    failures.push(`${label} declares hreflang alternates without an x-default`);
  }
  if (alternates.length && !(alternates.includes("en") && alternates.includes("zh-CN"))) {
    failures.push(`${label} declares an incomplete hreflang set: ${alternates.join(", ")}`);
  }
  const matches = text.matchAll(/(?:href|src)="([^"]+)"/g);
  for (const [, href] of matches) {
    if (configuredBase && href.startsWith("/") && !href.startsWith(configuredBasePrefix) && !href.startsWith("//")) {
      failures.push(`${label} escapes the configured base path: ${href}`);
    }
    const target = localTarget(href);
    if (target && !existsSync(target)) failures.push(`${label} has a broken local reference: ${href}`);
  }
}

/* This used to re-hash every artifact in dist/downloads/ and compare it against
 * the digest the release index claimed, catching a page that printed a checksum
 * the file did not actually have.
 *
 * That check has no subject any more. The site no longer hosts the artifacts —
 * it links to GitHub Releases and prints the digest the API reported, so there
 * is no local file to hash and nothing to compare a second opinion against. Be
 * aware of what that costs: a wrong digest from the release feed now reaches the
 * page unchallenged, where previously it could not survive the build.
 *
 * Restoring an equivalent gate means fetching each asset during validation and
 * hashing it, which is a network round trip per artifact.
 */
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log(`Validated ${htmlFiles.length} HTML pages.`);
