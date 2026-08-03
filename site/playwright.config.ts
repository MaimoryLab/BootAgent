import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  reporter: "list",
  /* Trace teardown writes its zip inside the per-test budget, and on a full
     three-project parallel run that write was itself timing out — turning
     passing tests into failures whose only error was "Fixture 'trace recording'
     timeout during teardown". The suite is ~1.4 min without it and was 4-7 min
     with it. Traces are still captured on failure; they just get their own room
     to finish, and the per-test budget is no longer shared with the recorder. */
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: "http://127.0.0.1:4321",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "pnpm run build && pnpm run preview --host 127.0.0.1 --port 4321",
    url: "http://127.0.0.1:4321",
    reuseExistingServer: process.env.CI !== "true",
  },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 1000 } } },
    { name: "tablet", use: { ...devices["Desktop Chrome"], viewport: { width: 768, height: 900 } } },
    { name: "mobile", use: { ...devices["Desktop Chrome"], viewport: { width: 375, height: 812 } } },
  ],
});
