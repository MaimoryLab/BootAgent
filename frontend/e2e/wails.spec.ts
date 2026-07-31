import { expect, test } from "@playwright/test";

test("startup opens the overview and onboarding uses generated Wails bindings", async ({ page }) => {
  const bindingMethodIDs = new Set<number>();
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "POST" || path !== "/wails/runtime") return;
    const call = request.postDataJSON() as { args?: { methodID?: unknown } };
    if (typeof call.args?.methodID === "number") bindingMethodIDs.add(call.args.methodID);
  });

  await page.goto("/");
  await expect(page.getByRole("heading", { name: "环境总览" })).toBeVisible();
  await page.getByRole("button", { name: "开始配置" }).click();

  await expect(page.getByRole("heading", { name: "选择 Agent" })).toBeVisible();
  await page.getByLabel("选择 Codex").check();
  await page.getByRole("button", { name: "继续" }).click();

  await page.getByRole("button", { name: "配置模型服务" }).click();
  await page.getByRole("button", { name: "继续" }).click();

  await page.getByLabel("API Key").fill("e2e-key");
  await page.getByRole("button", { name: "测试连接" }).click();
  await expect(page.getByRole("status")).toContainText("connection test passed");
  await page.getByRole("button", { name: "继续选择模型" }).click();

  await expect(page.getByRole("radio", { name: "oneagent-e2e-model" })).toBeVisible();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: "开始激活" }).click();

  await expect(page.getByRole("heading", { name: "激活完成" })).toBeVisible();
  await page.getByRole("button", { name: "进入总览" }).click();
  await expect(page.getByRole("heading", { name: "环境总览" })).toBeVisible();
  expect(bindingMethodIDs.size).toBeGreaterThanOrEqual(4);
});

test("Provider CRUD persists keys and feeds Agent configuration", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "激活环境" }).click();
  await page.getByLabel("选择 Codex").check();
  await page.getByRole("button", { name: "继续" }).click();
  await page.getByRole("button", { name: "配置模型服务" }).click();
  await page.getByRole("button", { name: "继续" }).click();

  await page.getByRole("button", { name: "新增 Provider" }).click();
  await expect(page.getByRole("heading", { name: "新增 Provider" })).toBeVisible();
  await page.getByLabel("Provider ID").fill("acme");
  await page.getByLabel("名称").fill("Acme");
  await page.getByLabel("OpenAI 兼容 Base URL").fill("https://api.acme.test/openai");
  await page.getByLabel("API Key").fill("sk-acme");
  await page.getByRole("button", { name: "保存", exact: true }).click();

  await expect(page.getByRole("heading", { name: "连接模型服务" })).toBeVisible();
  await page.getByLabel("模型服务").selectOption("acme");
  await expect(page.getByLabel("API Key")).toHaveValue("sk-acme");

  await page.getByRole("link", { name: "Provider" }).click();
  const card = page.getByTestId("provider-acme");
  await expect(card).toContainText("已保存 Key");
  await page.getByRole("button", { name: "编辑 Acme" }).click();
  await expect(page.getByLabel("API Key")).toHaveValue("sk-acme");
  await page.getByRole("button", { name: "关闭编辑" }).click();

  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "删除 Acme" }).click();
  await expect(page.getByTestId("provider-acme")).toHaveCount(0);
});
