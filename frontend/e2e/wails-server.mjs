import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const frontendDir = dirname(fileURLToPath(import.meta.url));
const root = join(frontendDir, "..", "..");
const home = await mkdtemp(join(tmpdir(), "bootagent-wails-e2e-"));
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
  await rm(home, { recursive: true, force: true });
  process.exit(code ?? 1);
});
