import { expect, test, type Page, type Route } from "@playwright/test";

const catalog = [
  { id: "codex", name: "Codex", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "0.145.0", platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "claude-code", name: "Claude Code", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "2.1.217", platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "opencode", name: "OpenCode", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "1.18.4", platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "kilo-cli", name: "Kilo CLI", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "7.4.11", platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "aider", name: "Aider", group: "auto", configMode: "auto", guideOnly: false, lockedVersion: "0.86.2", platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "openclaw", name: "OpenClaw", group: "gateway", configMode: "guide", guideOnly: true, lockedVersion: null, platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "cursor", name: "Cursor", group: "platform", configMode: "guide", guideOnly: true, lockedVersion: null, platforms: ["macos", "linux", "windows"], platformNote: "" },
  { id: "cline", name: "Cline", group: "ide", configMode: "guide", guideOnly: true, lockedVersion: null, platforms: ["macos", "linux", "windows"], platformNote: "" },
];

const agentStatuses = Object.fromEntries(
  catalog.map((agent, index) => [
    agent.id,
    {
      installed: index === 0 || index === 2,
      configured: false,
      guideOnly: agent.guideOnly,
      config: agent.guideOnly ? "" : `/tmp/home/.config/${agent.id}`,
      version: index === 0 || index === 2 ? agent.lockedVersion : null,
      lockedVersion: agent.lockedVersion,
      canInstall: !agent.guideOnly,
    },
  ]),
);

function statusPayload(activated: boolean) {
  return {
    apiVersion: 1,
    platform: { os: "macos", arch: "arm64", shell: "bash" },
    capabilities: { canInstall: {}, supportedAgentIds: catalog.map((agent) => agent.id) },
    agents: agentStatuses,
    catalog,
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
    paths: {
      profile: "/tmp/home/.oneagent/profile.json",
      codex_config: "/tmp/home/.codex/config.toml",
      "claude-code_config": "/tmp/home/.claude/settings.json",
      opencode_config: "/tmp/home/.config/opencode/opencode.jsonc",
    },
    backups: {},
    environment: activated
      ? {
          schema_version: 1,
          provider: "ppio",
          base_url: "https://api.ppio.com/openai",
          model: "deepseek-v3",
          config_mode: "provider",
          agent_ids: ["codex", "claude-code", "opencode"],
          activated_at: "2026-07-22T00:00:00Z",
        }
      : null,
    environmentError: null,
  };
}

async function fulfillJson(route: Route, body: object, status = 200) {
  await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
}

async function mockApi(page: Page, options: { failFirstInstall?: boolean } = {}) {
  let activated = false;
  let installCalls = 0;
  const installBodies: Record<string, unknown>[] = [];
  const probeBodies: Record<string, unknown>[] = [];
  const modelBodies: Record<string, unknown>[] = [];

  await page.route("**/api/status", (route) => fulfillJson(route, statusPayload(activated)));
  await page.route("**/api/probe", (route) => {
    probeBodies.push(route.request().postDataJSON() as Record<string, unknown>);
    return fulfillJson(route, { ok: true, reachable: true, status: 200, message: "连接测试通过", error_code: null, retryable: false });
  });
  await page.route("**/api/models", (route) => {
    modelBodies.push(route.request().postDataJSON() as Record<string, unknown>);
    return fulfillJson(route, { ok: true, reachable: true, status: 200, message: "Found 2 models.", error_code: null, retryable: false, models: ["deepseek-v3", "qwen3-coder"] });
  });
  await page.route("**/api/open-register", (route) => fulfillJson(route, { ok: true, url: "https://ppio.com/", message: "opened" }));
  await page.route("**/api/install", async (route) => {
    installCalls += 1;
    const body = route.request().postDataJSON() as Record<string, unknown>;
    installBodies.push(body);
    await new Promise((resolve) => setTimeout(resolve, 120));
    const agents = body.agents as string[];
    if (options.failFirstInstall && installCalls === 1) {
      await fulfillJson(route, {
        ok: false,
        code: 3,
        results: agents.map((agent, index) =>
          index === 0
            ? { agent, status: "failed", error_code: "PREREQUISITE_MISSING", message: "npm is required", retryable: true }
            : { agent, status: "configured", retryable: false },
        ),
        log: "redacted install log",
        next: "",
        probe: null,
      });
      return;
    }
    activated = true;
    await fulfillJson(route, {
      ok: true,
      code: 0,
      results: agents.map((agent) => ({ agent, status: body.configure ? "configured" : "skipped", retryable: false })),
      log: "redacted install log",
      next: "source ~/.oneagent/env && codex",
      probe: body.configure
        ? { ok: true, reachable: true, status: 200, message: "Connection test passed.", error_code: null, retryable: false }
        : null,
    });
  });

  return { installBodies, modelBodies, probeBodies };
}

async function expectNoHorizontalOverflow(page: Page) {
  const sizes = await page.evaluate(() => ({ body: document.body.scrollWidth, viewport: window.innerWidth }));
  expect(sizes.body).toBeLessThanOrEqual(sizes.viewport);
}

for (const viewport of [
  { width: 1440, height: 900, label: "1440x900" },
  { width: 1280, height: 800, label: "1280x800" },
  { width: 1024, height: 720, label: "1024x720" },
]) {
  test(`完整七页流程 ${viewport.label}`, async ({ page }, testInfo) => {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "选择 Agent" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`01-agents-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);

    await page.getByRole("checkbox", { name: "选择 Codex" }).check();
    await page.getByRole("checkbox", { name: "选择 Claude Code" }).check();
    await page.getByRole("checkbox", { name: "选择 OpenCode" }).check();
    await page.getByRole("button", { name: "继续" }).click();
    await expect(page.getByRole("heading", { name: "配置方式" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`02-mode-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);

    await page.getByRole("button", { name: /配置模型服务/ }).click();
    await page.getByRole("button", { name: "继续" }).click();
    await expect(page.getByRole("heading", { name: "连接模型服务" })).toBeVisible();
    await page.getByLabel("API Key").fill("sentinel-browser-secret");
    await page.getByRole("button", { name: "测试连接" }).click();
    await expect(page.getByText("连接测试通过")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`03-provider-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);

    await page.getByRole("button", { name: "继续选择模型" }).click();
    await expect(page.getByRole("heading", { name: "选择模型" })).toBeVisible();
    await expect(page.getByRole("radio", { name: /deepseek-v3/ })).toBeChecked();
    await page.screenshot({ path: testInfo.outputPath(`04-model-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);

    await page.getByRole("button", { name: "继续" }).click();
    await expect(page.getByRole("heading", { name: "确认激活" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`05-review-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);

    await page.getByRole("button", { name: "开始激活" }).click();
    await expect(page.getByRole("heading", { name: "激活完成" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`06-result-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);
    expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 });
    expect(await page.content()).not.toContain("sentinel-browser-secret");

    await page.getByRole("button", { name: "进入总览" }).click();
    await expect(page.getByText("开发环境已就绪")).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`07-overview-${viewport.label}.png`) });
    await expectNoHorizontalOverflow(page);
  });
}

test("已有账号路径跳过 Provider 和模型", async ({ page }) => {
  const mock = await mockApi(page);
  await page.goto("/");
  await page.getByRole("checkbox", { name: "选择 Codex" }).check();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: /使用已有账号或配置/ }).click();
  await page.getByRole("button", { name: "继续" }).click();
  await expect(page.getByRole("heading", { name: "确认激活" })).toBeVisible();
  await expect(page.getByText("已跳过")).toHaveCount(2);
  await page.getByRole("button", { name: "开始激活" }).click();
  await expect(page.getByRole("heading", { name: "激活完成" })).toBeVisible();
  expect(mock.installBodies[0]).toMatchObject({ configure: false, api_key: "", agents: ["codex"] });
});

test("切回内置 Provider 后不提交残留 Custom URL", async ({ page }) => {
  const mock = await mockApi(page);
  await page.goto("/");
  await page.getByRole("checkbox", { name: "选择 Codex" }).check();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: /配置模型服务/ }).click();
  await page.getByRole("button", { name: "继续" }).click();
  await expect(page.getByRole("heading", { name: "连接模型服务" })).toBeVisible();
  await page.getByRole("radio", { name: "自定义", exact: true }).click();
  await page.getByLabel("Base URL").fill("http://127.0.0.1:9900/openai");
  await page.getByRole("radio", { name: "PPIO", exact: true }).click();
  await page.getByLabel("API Key").fill("provider-switch-secret");
  await page.getByRole("button", { name: "测试连接" }).click();
  await expect(page.getByText("连接测试通过")).toBeVisible();
  await page.getByRole("button", { name: "继续选择模型" }).click();
  await expect(page.getByRole("heading", { name: "选择模型" })).toBeVisible();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: "开始激活" }).click();
  await expect(page.getByRole("heading", { name: "激活完成" })).toBeVisible();
  expect(mock.probeBodies[0]).toMatchObject({ provider: "ppio", api_base_url: "" });
  expect(mock.modelBodies[0]).toMatchObject({ provider: "ppio", api_base_url: "" });
  expect(mock.installBodies[0]).toMatchObject({ provider: "ppio", api_base_url: "" });
});

test("失败 Agent 可以单独重试且不重复成功项", async ({ page }) => {
  const mock = await mockApi(page, { failFirstInstall: true });
  await page.goto("/");
  await page.getByRole("checkbox", { name: "选择 Codex" }).check();
  await page.getByRole("checkbox", { name: "选择 OpenCode" }).check();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: /配置模型服务/ }).click();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByLabel("API Key").fill("retry-secret");
  await page.getByRole("button", { name: "继续选择模型" }).click();
  await expect(page.getByRole("heading", { name: "选择模型" })).toBeVisible();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: "开始激活" }).click();
  await expect(page.getByRole("heading", { name: "需要处理部分问题" })).toBeVisible();
  await page.getByRole("button", { name: "重试" }).click();
  await expect(page.getByRole("heading", { name: "激活完成" })).toBeVisible();
  expect(mock.installBodies).toHaveLength(2);
  expect(mock.installBodies[1]).toMatchObject({ agents: ["codex"], profile_agents: ["codex", "opencode"] });
});
