import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawn } from "node:child_process";

const frontendDir = dirname(fileURLToPath(import.meta.url));
const root = join(frontendDir, "..", "..");
const home = await mkdtemp(join(tmpdir(), "oneagent-wails-e2e-"));
const child = spawn("go", ["run", "-tags", "wails,server,e2e", "./cmd/oneagent-desktop"], {
  cwd: root,
  env: {
    ...process.env,
    ONEAGENT_HOME: home,
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
