import { expect, test } from "@playwright/test";

// Every test addresses a route directly. "/" is a decision, not a page: on a
// home without ~/.oneagent it opens onboarding, so landing there would make
// these tests depend on whichever one ran first and wrote state.
test("language selection switches to English and persists", async ({ page }) => {
  await page.goto("/#/overview");
  await page.getByRole("combobox", { name: "语言" }).selectOption("en");
  await expect(page.getByRole("heading", { name: "Environment overview" })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("combobox", { name: "Language" })).toHaveValue("en");
});

test("a machine with no OneAgent state opens onboarding from the landing route", async ({ page }) => {
  await page.goto("/#/");
  await expect(page.getByRole("heading", { name: "选择 Agent" })).toBeVisible();
});

test("onboarding installs one Agent end to end and writes its Profile", async ({ page }) => {
  const bindingMethodIDs = new Set<number>();
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "POST" || path !== "/wails/runtime") return;
    const call = request.postDataJSON() as { args?: { methodID?: unknown } };
    if (typeof call.args?.methodID === "number") bindingMethodIDs.add(call.args.methodID);
  });

  // Onboarding is the only way to create a Profile: it collects the Agent, key
  // and model in order and the install writes the Profile itself.
  await page.goto("/#/overview");
  await expect(page.getByText("尚未安装任何 Agent")).toBeVisible();
  await page.getByRole("button", { name: "安装 Agent" }).click();

  await expect(page.getByRole("heading", { name: "选择 Agent" })).toBeVisible();
  await page.getByLabel("选择 Codex").check();
  await page.getByRole("button", { name: "继续" }).click();

  await expect(page.getByRole("heading", { name: "连接模型服务" })).toBeVisible();
  await page.getByLabel("API Key").fill("e2e-key");
  await page.getByRole("button", { name: "测试连接" }).click();
  // The model step is gated on a passing probe, not merely a non-empty key.
  await page.getByRole("button", { name: "继续选择模型" }).click();

  await expect(page.getByRole("heading", { name: "选择模型" })).toBeVisible();
  await page.getByRole("radio", { name: /oneagent-e2e-model/ }).click();
  await page.getByRole("button", { name: "继续" }).click();

  await expect(page.getByRole("heading", { name: "确认激活" })).toBeVisible();
  await page.getByLabel("配置模板名称").fill("团队默认");
  await page.getByRole("button", { name: "开始安装" }).click();

  await expect(page.getByRole("heading", { name: "安装完成" })).toBeVisible({ timeout: 60_000 });
  await page.getByRole("button", { name: "进入总览" }).click();

  await expect(page.getByRole("heading", { name: "环境总览" })).toBeVisible();
  const agent = page.getByTestId("agent-codex");
  await expect(agent).toContainText("PPIO");
  await expect(agent).toContainText("团队默认");

  // The Profile the install wrote is editable, but no longer creatable inline.
  await page.getByRole("link", { name: "配置模板" }).click();
  await expect(page.getByText("团队默认")).toBeVisible();
  expect(bindingMethodIDs.size).toBeGreaterThanOrEqual(3);
});

test("Provider CRUD persists keys", async ({ page }) => {
  await page.goto("/#/providers");
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
