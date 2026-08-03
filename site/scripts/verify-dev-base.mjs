import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const host = "127.0.0.1";
const port = 4322;
const origin = `http://${host}:${port}`;
const canonicalOrigin = "https://oneagent.example";
const astroBin = fileURLToPath(new URL("../node_modules/astro/bin/astro.mjs", import.meta.url));
const child = spawn(process.execPath, [astroBin, "dev", "--host", host, "--port", String(port)], {
  cwd: new URL("..", import.meta.url),
  env: { ...process.env, SITE_URL: canonicalOrigin, BASE_PATH: "/" },
  stdio: ["ignore", "pipe", "pipe"],
});

let output = "";
child.stdout.on("data", (chunk) => { output += chunk; });
child.stderr.on("data", (chunk) => { output += chunk; });

async function waitForPage() {
  let lastError;
  for (let attempt = 0; attempt < 80; attempt += 1) {
    try {
      const response = await fetch(origin);
      if (response.ok) return response.text();
      lastError = new Error(`HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw lastError ?? new Error("Astro dev server did not start");
}

try {
  const html = await waitForPage();
  const base = html.match(/<base href="([^"]+)"/i)?.[1];
  const canonical = html.match(/<link rel="canonical" href="([^"]+)"/i)?.[1];
  assert.equal(base, `${origin}/`, "runtime <base> must follow the active dev origin");
  assert.equal(canonical, `${canonicalOrigin}/`, "canonical must continue to use SITE_URL");
  console.log(`Verified dev base ${base} with canonical ${canonical}`);
} catch (error) {
  console.error(output);
  throw error;
} finally {
  child.kill("SIGTERM");
  await new Promise((resolve) => {
    const stop = spawn(process.execPath, [astroBin, "dev", "stop"], {
      cwd: new URL("..", import.meta.url),
      env: { ...process.env, SITE_URL: canonicalOrigin, BASE_PATH: "/" },
      stdio: "ignore",
    });
    stop.once("exit", resolve);
    stop.once("error", resolve);
  });
}
