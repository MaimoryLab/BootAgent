import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const frontendDir = dirname(fileURLToPath(import.meta.url));
const root = join(frontendDir, "..", "..");
const home = await mkdtemp(join(tmpdir(), "bootagent-wails-e2e-"));
const upstream = createServer((request, response) => {
  if (request.url === "/v1/chat/completions") {
    request.resume();
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({
      id: "chatcmpl-e2e",
      object: "chat.completion",
      choices: [{ index: 0, message: { role: "assistant", content: "pong" }, finish_reason: "stop" }],
      usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
    }));
    return;
  }
  response.writeHead(404, { "content-type": "application/json" });
  response.end(JSON.stringify({ error: { message: "unsupported protocol" } }));
});
await new Promise((resolve, reject) => {
  upstream.once("error", reject);
  upstream.listen(34124, "127.0.0.1", resolve);
});
const child = spawn("go", ["run", "-tags", "wails,server,e2e", "./cmd/bootagent-desktop"], {
  cwd: root,
  env: {
    ...process.env,
    // Server-mode tests do not use a native WebView; keep Linux CI independent
    // of GTK/WebKitGTK development packages.
    CGO_ENABLED: "0",
    HOME: home,
    USERPROFILE: home,
  },
  stdio: "inherit",
});

let stopping = false;
const stop = () => {
  if (!stopping) {
    stopping = true;
    child.kill();
  }
};

process.once("SIGINT", stop);
process.once("SIGTERM", stop);
child.once("exit", async (code) => {
  if (upstream.listening) upstream.close();
  await rm(home, { recursive: true, force: true });
  process.exit(code ?? 1);
});
