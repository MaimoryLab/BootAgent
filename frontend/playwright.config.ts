import { defineConfig } from "@playwright/test";

const pythonCommand = process.platform === "win32" ? "python ..\\scripts\\gui.py --port 8765 --no-open" : "python3 ../scripts/gui.py --port 8765 --no-open";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:8765",
    browserName: "chromium",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: pythonCommand,
    cwd: process.cwd(),
    url: "http://127.0.0.1:8765/",
    reuseExistingServer: true,
    timeout: 30_000,
    env: {
      ONEAGENT_DISABLE_BROWSER: "1",
    },
  },
});
