import { expect, test } from "@playwright/test";
import type { Page, Route } from "@playwright/test";

import type { AgentStatus, StatusResponse } from "../src/types/api";

const AUTO_AGENTS = ["codex", "claude-code", "opencode", "kilo-cli", "aider"] as const;

function agentStatus(over: Partial<AgentStatus> = {}): AgentStatus {
  return {
    installed: true,
    configured: true,
    guideOnly: false,
    config: "/tmp/home/.config",
    version: "1.0.0",
    lockedVersion: "1.0.0",
    canInstall: true,
    provider: null,
    model: null,
    baseUrl: null,
    updatedAt: null,
    ...over,
  };
}

/** Two Agents on different providers — the state per-Agent config exists for. */
function divergentStatus(): StatusResponse {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    capabilities: { canInstall: {}, supportedAgentIds: [...AUTO_AGENTS] },
    agents: {
      codex: agentStatus({
        provider: "ppio",
        model: "deepseek/deepseek-v3",
        baseUrl: "https://api.ppio.com/openai",
        version: "0.144.4",
        lockedVersion: "0.145.0",
      }),
      "claude-code": agentStatus({ provider: "novita", model: "qwen/qwen3-max" }),
      opencode: agentStatus({ configured: false }),
      "kilo-cli": agentStatus({ installed: false, configured: false }),
      aider: agentStatus({ installed: false, configured: false }),
      cursor: agentStatus({ guideOnly: true, configured: false }),
    },
    catalog: [
      ...AUTO_AGENTS.map((id) => ({
        id,
        name: id === "claude-code" ? "Claude Code" : id === "kilo-cli" ? "Kilo CLI" : id[0].toUpperCase() + id.slice(1),
        group: "auto" as const,
        configMode: "auto" as const,
        guideOnly: false,
        lockedVersion: id === "codex" ? "0.145.0" : "1.0.0",
        protocol: id === "codex" ? ("responses" as const) : ("openai" as const),
        platforms: ["macos" as const, "linux" as const, "windows" as const],
        platformNote: "",
      })),
      {
        id: "cursor",
        name: "Cursor",
        group: "ide" as const,
        configMode: "guide" as const,
        guideOnly: true,
        lockedVersion: null,
        protocol: null,
        platforms: ["macos" as const],
        platformNote: "按官方文档配置",
      },
    ],
    groups: [
      { id: "auto", name: "One-click configurable" },
      { id: "gateway", name: "Gateway agents" },
      { id: "platform", name: "Official account agents" },
      { id: "ide", name: "IDE extensions" },
    ],
    providers: {
      ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" },
      novita: { name: "Novita", home: "https://novita.ai/", base_url: "https://api.novita.ai/openai" },
    },
    paths: { profile: "/tmp/home/.oneagent/profile.json" },
    backups: {},
    environment: null,
    environmentError: null,
    profiles: [],
    activeProfile: null,
  };
}

async function fulfillJson(route: Route, body: object, status = 200) {
  await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
}

async function mockOverview(page: Page) {
  const activateBodies: Record<string, unknown>[] = [];
  const activatePaths: string[] = [];

  await page.route("**/api/status", (route) => fulfillJson(route, divergentStatus()));
  await page.route("**/api/probe", (route) =>
    fulfillJson(route, {
      ok: true,
      reachable: true,
      status: 200,
      message: "连接测试通过",
      error_code: null,
      retryable: false,
    }),
  );
  await page.route("**/api/agents/*/activate", (route) => {
    activatePaths.push(new URL(route.request().url()).pathname);
    activateBodies.push(route.request().postDataJSON() as Record<string, unknown>);
    return fulfillJson(route, {
      ok: true,
      agent: "codex",
      config: "/tmp/home/.codex/config.toml",
      provider: "novita",
      model: "qwen/qwen3-max",
      restart: "Quit any running codex process, then start it again",
      next: "source ~/.oneagent/agents/codex.env && codex",
    });
  });
  return { activateBodies, activatePaths };
}

test("总览按 Agent 分别显示各自的 Provider 与模型", async ({ page }, testInfo) => {
  await mockOverview(page);
  await page.goto("/#/overview");

  // The whole point of per-Agent config: two Agents, two providers, at once.
  await expect(page.getByText("PPIO", { exact: false }).first()).toBeVisible();
  await expect(page.getByText("qwen/qwen3-max")).toBeVisible();
  await expect(page.getByText("deepseek/deepseek-v3")).toBeVisible();

  // Version drift reads inline on the Agent's own row: it is deferrable
  // maintenance, not an alert that earns a banner at the top of the page.
  await expect(page.getByText(/0\.144\.4 → 0\.145\.0/)).toBeVisible();
  await expect(page.getByText("未配置").first()).toBeVisible();

  await page.screenshot({ path: testInfo.outputPath("overview-per-agent.png"), fullPage: true });
});

test("配置在 Agent 独立页面完成，成功后给出重启指引", async ({ page }, testInfo) => {
  const { activateBodies, activatePaths } = await mockOverview(page);
  await page.goto("/#/overview");

  // Configuring moved off the list: a form inside a row grew the list by its
  // own height and allowed several rows to sit half-configured at once.
  const codexRow = page.locator(".agent-manage-row").first();
  await expect(codexRow.getByLabel("API Key")).toHaveCount(0);
  await codexRow.click();

  await expect(page).toHaveURL(/#\/agents\/codex$/);
  await expect(page.getByRole("heading", { name: "Codex" })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("detail-open.png"), fullPage: true });

  const apply = page.getByRole("button", { name: /^应用/ });
  await expect(apply).toBeDisabled();

  await page.getByLabel("API Key").fill("sentinel-detail-secret");
  await page.getByRole("button", { name: "测试连接" }).click();
  await expect(apply).toBeEnabled();

  await apply.click();
  // An Agent reads its config at startup, so the switch is invisible until the
  // process restarts; reporting success without that reads as a failure.
  await expect(page.getByText(/Quit any running codex/)).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("detail-applied.png"), fullPage: true });

  expect(activatePaths).toEqual(["/api/agents/codex/activate"]);
  expect(activateBodies).toHaveLength(1);

  await expect(page.getByLabel("API Key")).toHaveValue("");
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({
    local: 0,
    session: 0,
  });
});

test("首屏用于 Agent 列表，而不是横幅与提醒", async ({ page }, testInfo) => {
  await mockOverview(page);
  await page.setViewportSize({ width: 1280, height: 860 });
  await page.goto("/#/overview");
  await page.waitForSelector(".agent-manage-row");

  // The overview is opened every day, so its first screen belongs to the
  // Agents. It used to spend 55% of the viewport on a one-off "ready" banner
  // and a version reminder before the list even started.
  const list = await page.locator(".agent-manage-list").boundingBox();
  expect(list).not.toBeNull();
  expect(list!.y).toBeLessThan(220);

  // All five managed Agents fit without scrolling.
  const rows = page.locator(".agent-manage-row");
  await expect(rows).toHaveCount(5);
  const last = await rows.last().boundingBox();
  expect(last!.y + last!.height).toBeLessThan(860);

  // The dismissed banners are gone, not merely moved.
  await expect(page.getByText("开发环境已就绪")).toHaveCount(0);
  await expect(page.locator(".overview-notice")).toHaveCount(0);

  await page.screenshot({ path: testInfo.outputPath("overview-first-screen.png") });
});

test("侧边栏每一项都能真的打开对应页面", async ({ page }, testInfo) => {
  await mockOverview(page);
  await page.goto("/#/overview");
  await page.waitForSelector(".agent-manage-row");

  // Provider and 配置模板 used to point at wizard steps behind SetupGuard, so
  // clicking them bounced back to step one and looked like a dead link.
  for (const [label, hash, heading] of [
    ["Provider", "#/providers", "Provider"],
    ["配置模板", "#/profiles", "配置模板"],
    ["环境总览", "#/overview", "环境总览"],
  ] as const) {
    await page.getByRole("link", { name: label }).click();
    await expect(page).toHaveURL(new RegExp(hash.replace("/", "\\/")));
    await expect(page.getByRole("heading", { name: heading })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`nav-${heading}.png`), fullPage: true });
  }
});

test("Provider 页反查出每个服务正在被哪些 Agent 使用", async ({ page }) => {
  await mockOverview(page);
  await page.goto("/#/providers");
  const ppio = page.getByTestId("provider-ppio");
  await expect(ppio).toContainText("Codex");
  const novita = page.getByTestId("provider-novita");
  await expect(novita).toContainText("Claude Code");
});

test("已配置的环境直接进入总览而不是向导", async ({ page }) => {
  await mockOverview(page);
  await page.goto("/");
  // A returning user should not be sent back through first-run setup.
  await expect(page).toHaveURL(/#\/overview$/);
  await expect(page.getByRole("heading", { name: "环境总览" })).toBeVisible();
});
