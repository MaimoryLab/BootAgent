import { expect, test } from "@playwright/test";

test("language selection switches to English and persists", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("combobox", { name: "语言" }).selectOption("en");
  await expect(page.getByRole("heading", { name: "Environment overview" })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("combobox", { name: "Language" })).toHaveValue("en");
});

test("Profile management applies an environment and the overview stays read-only", async ({ page }) => {
  const bindingMethodIDs = new Set<number>();
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "POST" || path !== "/wails/runtime") return;
    const call = request.postDataJSON() as { args?: { methodID?: unknown } };
    if (typeof call.args?.methodID === "number") bindingMethodIDs.add(call.args.methodID);
  });

  await page.goto("/");
  await expect(page.getByRole("heading", { name: "环境总览" })).toBeVisible();
  await expect(page.getByText("尚未安装任何 Agent")).toBeVisible();
  await expect(page.getByRole("button", { name: /开始配置|新建配置/ })).toHaveCount(0);

  await page.getByRole("link", { name: "配置模板" }).click();
  await page.getByRole("button", { name: "新增 Profile" }).click();
  await page.getByLabel("Profile ID").fill("team-ppio");
  await page.getByLabel("名称").fill("团队 PPIO");
  await page.getByLabel("模型", { exact: true }).fill("oneagent-e2e-model");
  await page.getByLabel("API Key").fill("e2e-key");
  await page.getByLabel("选择 Codex").check();
  await page.getByRole("button", { name: "保存 Profile" }).click();

  const profile = page.getByTestId("profile-team-ppio");
  await expect(profile).toContainText("团队 PPIO");
  await profile.getByRole("button", { name: "编辑 团队 PPIO" }).click();
  await page.getByLabel("名称").fill("团队默认");
  await page.getByRole("button", { name: "保存 Profile" }).click();
  await expect(profile).toContainText("团队默认");
  await profile.getByRole("button", { name: "应用到 Agent" }).click();
  await expect(page.getByText(/已应用到 1 个 Agent/)).toBeVisible();

  await page.getByRole("link", { name: "激活环境" }).click();
  await expect(page.getByRole("heading", { name: "环境总览" })).toBeVisible();
  const agent = page.getByTestId("agent-codex");
  await expect(agent).toContainText("PPIO");
  await expect(agent).toContainText("团队默认");
  expect(bindingMethodIDs.size).toBeGreaterThanOrEqual(3);
});

test("Provider CRUD persists keys", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Provider" }).click();
  await page.getByRole("button", { name: "新增 Provider" }).click();
  await expect(page.getByLabel("Provider ID")).toBeVisible();
  await page.getByLabel("Provider ID").fill("acme");
  await page.getByLabel("名称").fill("Acme");
  await page.getByLabel("OpenAI 兼容 Base URL").fill("https://api.acme.test/openai");
  await page.getByLabel("API Key").fill("sk-acme");
  await page.getByRole("button", { name: "保存", exact: true }).click();

  const card = page.getByTestId("provider-acme");
  await expect(card).toContainText("已保存 Key");
  await page.getByRole("button", { name: "编辑 Acme" }).click();
  await expect(page.getByLabel("API Key")).toHaveValue("sk-acme");
  await page.getByRole("button", { name: "关闭编辑" }).click();

  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "删除 Acme" }).click();
  await expect(page.getByTestId("provider-acme")).toHaveCount(0);
});
