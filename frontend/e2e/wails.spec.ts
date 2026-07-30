import { expect, test } from "@playwright/test";

test("the onboarding flow uses generated Wails bindings", async ({ page }) => {
  const bindingMethodIDs = new Set<number>();
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "POST" || path !== "/wails/runtime") return;
    const call = request.postDataJSON() as { args?: { methodID?: unknown } };
    if (typeof call.args?.methodID === "number") bindingMethodIDs.add(call.args.methodID);
  });

  await page.goto("/");
  await expect(page.getByRole("link", { name: "Start with OneAgent" })).toBeVisible();
  await page.getByRole("link", { name: "Start with OneAgent" }).click();

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
