import { expect, test } from "@playwright/test";

// Every test addresses a route directly. "/" is a decision, not a page: on a
// home without ~/.oneagent it opens onboarding, so landing there would make
// these tests depend on whichever one ran first and wrote state.
// selectOption is gone from these tests: the pickers are custom listboxes now,
// because a native select's popup is drawn by the OS and cannot be styled. The
// combobox role is unchanged, so they are still found the same way, but choosing
// takes the two steps a user takes.
test("language selection switches to English and persists", async ({ page }) => {
  await page.goto("/#/overview");
  await page.getByRole("combobox", { name: "语言" }).click();
  await page.getByRole("option", { name: "English" }).click();
  await expect(page.getByRole("heading", { name: "Environment overview" })).toBeVisible();

  await page.reload();
  // The trigger shows the current value, which is also the check that the choice
  // survived a reload rather than only re-rendering the heading.
  await expect(page.getByRole("combobox", { name: "Language" })).toHaveText("English");
});

// The task centre is position: fixed at the viewport's lower-left corner, so it
// takes no space in the sidebar's flex column. When the sidebar did not reserve
// room for it, the rows margin-top: auto pushes to the bottom ended up
// underneath: still rendered, still visible, still in the accessibility tree,
// with every click landing on the overlay. The language picker was the one that
// lost, because the theme row cleared it by 2px.
//
// This has to be an e2e test. jsdom reports every rect as 0x0, so a unit test
// cannot see the collision, and asserting display or visibility would have
// passed throughout -- both were correct the whole time.
test("every sidebar control at the bottom is actually clickable", async ({ page }) => {
  // Both sidebar widths: 204px above the 900px breakpoint, and the 72px icon
  // rail below it, where the task centre and the selects change size.
  for (const viewport of [{ width: 1180, height: 760 }, { width: 860, height: 600 }]) {
    await page.setViewportSize(viewport);
    await page.goto("/#/overview");
    const label = `${viewport.width}x${viewport.height}`;

    const covered = await page.evaluate(() => {
      const selectors = [".theme-select .select-field-trigger", ".language-select .select-field-trigger", ".task-center-trigger"];
      const blocked: Array<{ selector: string; coveredBy: string }> = [];
      for (const selector of selectors) {
        const element = document.querySelector(selector);
        if (!element) continue;
        const box = element.getBoundingClientRect();
        if (box.width === 0 || box.height === 0) continue;
        const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
        if (hit !== element && !element.contains(hit)) {
          blocked.push({ selector, coveredBy: hit ? `${hit.tagName}.${hit.className}` : "nothing" });
        }
      }
      return blocked;
    });
    expect(covered, `controls covered at ${label}`).toEqual([]);

    // A real pointer click: the regression was that the element stayed reachable
    // programmatically while being unreachable by pointer, so dispatching events
    // directly would have passed. Opening also proves the list is not itself
    // clipped by the sidebar or covered by the task centre.
    await page.getByRole("combobox", { name: /语言|Language/ }).click({ timeout: 2000 });
    await expect(page.getByRole("listbox", { name: /语言|Language/ })).toBeVisible();
    await page.keyboard.press("Escape");
  }
});

test("a machine with no OneAgent state opens onboarding from the landing route", async ({ page }) => {
  await page.goto("/#/");
  await expect(page.getByRole("heading", { name: "选择 Agent", level: 1 })).toBeVisible();
});

test("onboarding installs one Agent end to end and writes its Profile", async ({ page }) => {
  const bindingMethodIDs = new Set<number>();
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() !== "POST" || path !== "/wails/runtime") return;
    const call = request.postDataJSON() as { args?: { methodID?: unknown } };
    if (typeof call.args?.methodID === "number") bindingMethodIDs.add(call.args.methodID);
  });

  // Provider credentials are saved once on the Provider page. The wizard only
  // selects that Provider, optionally probes with a model ID, and saves the
  // resulting Profile.
  await page.goto("/#/providers");
  await page.getByRole("button", { name: "编辑 PPIO" }).click();
  await page.getByLabel("API Key").fill("e2e-key");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByTestId("provider-ppio")).toContainText("已保存 Key");

  await page.goto("/#/overview");
  await expect(page.getByText("尚未安装任何命令行 Agent")).toBeVisible();
  await page.getByRole("button", { name: "安装命令行 Agent" }).click();

  await expect(page.getByRole("heading", { name: "选择 Agent", level: 1 })).toBeVisible();
  await page.getByLabel("选择 Codex").check();
  await page.getByRole("button", { name: "继续" }).click();

  await expect(page.getByRole("heading", { name: "连接模型服务" })).toBeVisible();
  await page.getByLabel("自定义模型名称（可选）").fill("oneagent-e2e-model");
  await page.getByRole("button", { name: "测试连接" }).click();
  // The model step is gated on a passing Provider probe.
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

  // The Profile the install wrote is editable from the Agent row, without any
  // credential or endpoint fields on the edit page.
  await page.getByRole("link", { name: "配置", exact: true }).click();
  await expect(page.getByRole("heading", { name: "选择配置模板" })).toBeVisible();
  await expect(page.getByLabel("API Key")).toHaveCount(0);
  await expect(page.getByText("Base URL")).toHaveCount(0);
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
