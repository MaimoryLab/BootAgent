import { defineConfig } from "@playwright/test";

const port = Number(process.env.ONEAGENT_E2E_PORT || 34123);

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    locale: "zh-CN",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "pnpm run build && node ./e2e/wails-server.mjs",
    url: `http://127.0.0.1:${port}/health`,
    timeout: 120_000,
    reuseExistingServer: false,
    env: {
      WAILS_SERVER_HOST: "127.0.0.1",
      WAILS_SERVER_PORT: String(port),
    },
  },
});
