import { test, expect, type Page } from "@playwright/test";
import axe from "axe-core";

const criticalPages = ["/", "/explore/", "/downloads/", "/agents/", "/security/"];

/* Mirrors translatedRoutes in src/i18n/index.ts. It cannot be imported here:
   that module reads import.meta.env.BASE_URL, which only exists under Vite, and
   Playwright runs this file in plain Node. The englishPages list below is the
   guard — every route named here has to have an /en/ page that gets audited, so
   the two drifting apart fails rather than going unnoticed. */
const translatedRoutes = ["", "downloads/", "quickstart/", "explore/", "security/"];

test("critical pages fit the viewport without horizontal scrolling", async ({ page }) => {
  for (const path of criticalPages) {
    await page.goto(path);
    const dimensions = await page.evaluate(() => ({
      viewport: document.documentElement.clientWidth,
      content: document.documentElement.scrollWidth,
    }));
    expect(dimensions.content, `${path} overflows its ${dimensions.viewport}px viewport`).toBeLessThanOrEqual(dimensions.viewport);
  }
});

test("home presents one activation entry without claiming a real scan", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("激活你的 AI 开发环境");
  await expect(page.locator(".hero-actions").getByRole("button", { name: "开始激活演示" })).toBeVisible();
  await expect(page.locator(".hero-actions").getByRole("link")).toHaveCount(0);
  /* The console used to sit at idle until clicked. It now plays itself once it
     scrolls into view, so the successor guarantee is that the page drives the
     demo — the button stays as the replay and takeover entry. Scrolled here
     explicitly because whether the console starts on screen varies by viewport,
     and this test is not the one about the trigger threshold. */
  const console = page.locator("#activation-console");
  await console.scrollIntoViewIfNeeded();
  await expect(console).toHaveAttribute("data-autoplaying", "true");
  await expect(page.getByText("示例环境", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("不访问设备", { exact: true })).toBeVisible();
  await expect(page.locator('input[type="password"], input[name*="key" i]')).toHaveCount(0);
  await expect(page.locator(".hero-note")).toContainText("未签名技术预览版");
});

test("activation demo reaches Ready only for a supported managed combination", async ({ page }) => {
  const fetches: string[] = [];
  page.on("request", (request) => {
    if (["fetch", "xhr"].includes(request.resourceType())) fetches.push(request.url());
  });
  await page.goto("/");
  const console = page.locator("#activation-console");
  await page.getByRole("button", { name: "开始激活演示" }).click();
  await expect(console).toHaveAttribute("data-phase", "agent");
  await console.getByRole("button", { name: /Claude Code/ }).click();
  await expect(console).toHaveAttribute("data-phase", "mode");
  await console.getByRole("button", { name: /配置模型服务/ }).click();
  await expect(console).toHaveAttribute("data-phase", "provider");
  await console.getByRole("button", { name: /PPIO/ }).click();
  await console.getByRole("button", { name: "验证示例连接" }).click();
  await expect(console).toHaveAttribute("data-phase", "model");
  await console.getByRole("button", { name: "deepseek/deepseek-v3" }).click();
  await console.getByRole("button", { name: "确认激活" }).click();
  await expect(console).toHaveAttribute("data-phase", "ready");
  await expect(console.getByRole("heading", { name: "示例环境已 Ready" })).toBeVisible();
  await expect(console.getByRole("link", { name: "下载 OneAgent" })).toBeVisible();
  await expect(console.getByRole("link", { name: "打开完整配置目录" })).toBeVisible();
  await page.addScriptTag({ content: axe.source });
  const result = await page.evaluate(async () => {
    const axeApi = (window as typeof window & { axe: typeof axe }).axe;
    return axeApi.run(document.querySelector("#activation-console")!, {
      runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] },
    });
  });
  const serious = result.violations.filter(
    (violation) => violation.impact === "serious" || violation.impact === "critical",
  );
  expect(serious, JSON.stringify(serious, null, 2)).toEqual([]);
  expect(fetches, "the public demo must not call a local or remote activation API").toEqual([]);
});

test("activation demo preserves guide-only and preview-gate boundaries", async ({ page }) => {
  await page.goto("/");
  const console = page.locator("#activation-console");

  await page.getByRole("button", { name: "开始激活演示" }).click();
  await expect(console).toHaveAttribute("data-phase", "agent");
  await console.getByRole("button", { name: /Cursor/ }).click();
  await expect(console).toHaveAttribute("data-phase", "guide-only");
  await expect(console.getByRole("heading", { name: "转入官方设置" })).toBeVisible();
  await expect(console.getByRole("link", { name: "下载 OneAgent" })).toBeHidden();

  await console.getByRole("button", { name: "重置演示" }).click();
  await page.getByRole("button", { name: "开始激活演示" }).click();
  await expect(console).toHaveAttribute("data-phase", "agent");
  await console.getByRole("button", { name: /Codex/ }).click();
  await console.getByRole("button", { name: /配置模型服务/ }).click();
  await console.getByRole("button", { name: /PPIO/ }).click();
  await console.getByRole("button", { name: "验证示例连接" }).click();
  await expect(console).toHaveAttribute("data-phase", "preview-gate");
  await expect(console.getByRole("heading", { name: "仍需通过发布门禁" })).toBeVisible();
  await expect(console.getByRole("link", { name: "下载 OneAgent" })).toBeHidden();
  await expect(console.getByRole("link", { name: "发行政策" })).toBeVisible();
});

// The custom endpoint is the demo's only typed input, and its whole point is
// that the browser applies the same rules as oneagent/providers.py. A field that
// accepted anything would teach visitors an endpoint works when the app rejects it.
test("activation demo validates a custom endpoint the way the app does", async ({ page }) => {
  const fetches: string[] = [];
  page.on("request", (request) => {
    if (["fetch", "xhr"].includes(request.resourceType())) fetches.push(request.url());
  });
  await page.goto("/");
  const console = page.locator("#activation-console");
  await page.getByRole("button", { name: "开始激活演示" }).click();
  await console.getByRole("button", { name: /Claude Code/ }).click();
  await console.getByRole("button", { name: /配置模型服务/ }).click();
  await console.getByRole("button", { name: /自定义端点/ }).click();

  const url = console.getByLabel("Base URL");
  const verify = console.getByRole("button", { name: "验证示例连接" });
  await expect(url).toBeVisible();
  await expect(verify).toBeDisabled();

  for (const [value, message] of [
    ["ftp://api.example.com", "需要以 http:// 或 https:// 开头。"],
    ["https://user:secret@api.example.com", "请从地址中去掉用户名或密码。"],
  ]) {
    await url.fill(value);
    await expect(console.locator("[data-custom-hint]")).toHaveText(message);
    await expect(url).toHaveAttribute("aria-invalid", "true");
    await expect(verify, `${value} must not be verifiable`).toBeDisabled();
  }

  await url.fill("https://api.example.com/openai");
  await expect(console.locator("[data-custom-hint]")).toHaveText("端点可用");
  await expect(verify).toBeEnabled();
  await verify.click();

  // A custom endpoint publishes no catalog, so the demo shows the app's real
  // recovery — type the id — rather than inventing a discovered list.
  await expect(console).toHaveAttribute("data-phase", "model");
  await expect(console.locator("[data-model-grid]")).toBeHidden();
  await console.getByLabel("模型 ID").fill("deepseek/deepseek-v3");
  await console.getByRole("button", { name: "确认激活" }).click();
  await expect(console).toHaveAttribute("data-phase", "ready");
  await expect(console.locator("[data-result-provider]")).toHaveText("api.example.com");
  expect(fetches, "typing an endpoint must not make the page call it").toEqual([]);
});

// Reusing an existing account is a real branch in the product's ConfigModePage,
// and it exists precisely because no new credential is introduced — so the demo
// must not walk it through provider and model steps it genuinely skips.
test("activation demo skips provider and model for an existing account", async ({ page }) => {
  await page.goto("/");
  const console = page.locator("#activation-console");
  await page.getByRole("button", { name: "开始激活演示" }).click();
  await console.getByRole("button", { name: /Claude Code/ }).click();
  await expect(console).toHaveAttribute("data-phase", "mode");
  await console.getByRole("button", { name: /使用已有账号或配置/ }).click();

  await expect(console).toHaveAttribute("data-phase", "ready");
  await expect(console.getByRole("heading", { name: "保留现有账号" })).toBeVisible();
  await expect(console.locator(".activation-steps li.is-skipped")).toHaveCount(2);
  await expect(console.locator("[data-result-provider]")).toHaveText("已跳过");
  await expect(console.locator("[data-log]")).toContainText("Provider 与模型步骤已跳过");
});

test("activation demo keeps a complete event history under reduced motion", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  const console = page.locator("#activation-console");
  /* Advancing the interface on its own is motion, so reduced motion means the
     console waits to be asked. That is also what keeps the rest of this test
     valid: nothing has moved off idle before the first click. */
  await console.scrollIntoViewIfNeeded();
  await expect(console).toHaveAttribute("data-phase", "idle");
  await expect(console).not.toHaveAttribute("data-autoplaying", "true");
  await page.getByRole("button", { name: "开始激活演示" }).click();
  await expect(console).toHaveAttribute("data-phase", "agent");
  await expect(console.locator("[data-log] li")).toHaveCount(2);
  await console.getByRole("button", { name: /Claude Code/ }).click();
  await console.getByRole("button", { name: /配置模型服务/ }).click();
  await console.getByRole("button", { name: /PPIO/ }).click();
  await console.getByRole("button", { name: "验证示例连接" }).click();
  await expect(console).toHaveAttribute("data-phase", "model");
  await console.getByRole("button", { name: "deepseek/deepseek-v3" }).click();
  await console.getByRole("button", { name: "确认激活" }).click();
  await expect(console).toHaveAttribute("data-phase", "ready");
  await expect(console.locator("[data-log]")).toContainText("示例协议验证完成");
  const animations = await console.evaluate((element) =>
    element.getAnimations({ subtree: true }).filter((animation) => animation.playState === "running").length,
  );
  expect(animations).toBe(0);
});

/* The landing page has to show what activation looks like to someone who clicks
   nothing at all — that is the whole reason autoplay exists. */
test("activation demo plays itself through to Ready without a click", async ({ page }) => {
  const fetches: string[] = [];
  page.on("request", (request) => {
    if (["fetch", "xhr"].includes(request.resourceType())) fetches.push(request.url());
  });
  await page.goto("/");
  const console = page.locator("#activation-console");
  await console.scrollIntoViewIfNeeded();

  await expect(console).toHaveAttribute("data-phase", "ready");
  await expect(console.getByRole("heading", { name: "示例环境已 Ready" })).toBeVisible();
  // Reaching the end clears the flag, so the visitor is in charge from here.
  await expect(console).not.toHaveAttribute("data-autoplaying", "true");
  await expect(console.locator("[data-log]")).toContainText("示例环境可进入 Ready");
  expect(fetches, "an unattended demo must not call anything either").toEqual([]);
});

// Autoplay is a demonstration, not a ride: the moment someone reaches for the
// console it belongs to them, and it must not resume behind their back.
test("interacting during autoplay stops it where it stands", async ({ page }) => {
  await page.goto("/");
  const console = page.locator("#activation-console");
  await console.scrollIntoViewIfNeeded();
  await expect(console).toHaveAttribute("data-autoplaying", "true");

  /* Takes over at a step that is waiting on a choice, then asserts the phase
     stops changing.
   *
   * `scanning` has to be excluded, and not because of timing: taking over mid-scan
   * still lands on `agent`, since start()'s own 650ms timer resolves the scan the
   * same way a clicked run would. Leaving a spinner up forever would be the worse
   * behaviour, so that transition is correct — it just is not "autoplay resumed",
   * which is what this test is about.
   *
   * Reading the phase and comparing it against itself, rather than waiting for one
   * named step and asserting that name later, is what keeps this stable. The
   * earlier version waited for `agent` and expected `agent` 2.5s on; but `agent`
   * lasts only ~850ms (scan resolves into it near t=1070ms, the script leaves it
   * at t=1920ms), so under the full suite's three-viewport load the click landed
   * after the script had moved on and the assertion failed on a name mismatch. */
  /* Waits for a step that is genuinely waiting on input. `idle` and `scanning`
     both fail the "not scanning" test for different reasons — one has not started,
     the other resolves on its own — so the wait is written as a positive match on
     the steps where the demo is parked on a choice. Whichever one the script has
     reached by click time is fine; the assertion below compares against it rather
     than against a fixed name. */
  await expect(console).toHaveAttribute("data-phase", /^(agent|mode|provider|model|review)$/);
  await console.locator("[data-log]").click();
  await expect(console).not.toHaveAttribute("data-autoplaying", "true");
  const takenOverAt = await console.getAttribute("data-phase");
  expect(takenOverAt, "takeover must be measured at a decision point").not.toBe("scanning");
  // Longer than the widest gap in AUTOPLAY_SCRIPT (1500ms), so a resumed script
  // would have advanced at least one step by the time this returns.
  await page.waitForTimeout(2500);
  expect(await console.getAttribute("data-phase"), "autoplay resumed after takeover").toBe(takenOverAt);

  // Taken over, not broken: the trigger still replays from the top.
  await page.getByRole("button", { name: "开始激活演示" }).click();
  await expect(console).toHaveAttribute("data-phase", "agent");
  await expect(console).not.toHaveAttribute("data-autoplaying", "true");
});

/* The failure this guards is specific and easy to reintroduce: every step calls
   focusPanel, and if autoplay keeps doing that it steals focus from whatever the
   visitor is reading or tabbing through — on a page they never interacted with. */
test("autoplay never takes keyboard focus", async ({ page }) => {
  await page.goto("/");
  const console = page.locator("#activation-console");
  await console.scrollIntoViewIfNeeded();

  const insideConsole = () =>
    page.evaluate(() => Boolean(document.querySelector("#activation-console")?.contains(document.activeElement)));
  for (let sample = 0; sample < 12; sample += 1) {
    expect(await insideConsole(), "autoplay moved focus into the console").toBe(false);
    await page.waitForTimeout(250);
  }
  await expect(console).toHaveAttribute("data-phase", "ready");
});

test("Explorer restores shareable state and returns focus after the drawer closes", async ({ page }) => {
  await page.goto("/explore/?agent=claude-code&provider=ppio&platform=macos&protocol=anthropic");
  const explorer = page.locator("compatibility-explorer");
  const drawer = explorer.locator("[data-explorer-drawer]");
  await expect(drawer).toBeVisible();
  await expect(drawer.getByRole("heading", { name: "Claude Code" })).toBeVisible();
  await expect(explorer.locator('[data-filter="platform"]')).toHaveValue("macos");
  await expect(explorer.locator('[data-filter="provider"]')).toHaveValue("ppio");
  await expect(explorer.locator('[data-filter="protocol"]')).toHaveValue("anthropic");

  await drawer.getByRole("button", { name: "关闭详情" }).click();
  await expect(page).not.toHaveURL(/agent=/);

  const card = explorer.locator('[data-agent-card][data-agent-id="claude-code"]');
  await card.focus();
  await page.keyboard.press("Enter");
  await expect(drawer).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(drawer).toBeHidden();
  await expect(card).toBeFocused();
});

test("Explorer filters only catalog-backed combinations", async ({ page }) => {
  await page.goto("/explore/");
  const explorer = page.locator("compatibility-explorer");
  await explorer.locator('[data-filter="platform"]').selectOption("windows");
  await explorer.locator('[data-filter="protocol"]').selectOption("responses");
  await explorer.locator('[data-filter="provider"]').selectOption("ppio");
  const visible = explorer.locator("[data-agent-card]:visible");
  await expect(visible).toHaveCount(1);
  await expect(visible).toHaveAttribute("data-agent-id", "codex");
  await expect(page).toHaveURL(/platform=windows/);
  await expect(page).toHaveURL(/protocol=responses/);
  await expect(page).toHaveURL(/provider=ppio/);
});

/* The download page renders from whatever the GitHub Releases feed returned at
   build time. With no published release the whole platform picker is replaced by
   a "not published yet" notice, which is the correct page — but it means the
   picker tests have nothing to drive.
 *
 * They skip rather than being deleted or loosened. A release exists in the repo
 * this site ships from, so these assertions are the ones that catch a regression
 * there; silently dropping them would mean the download flow rots unnoticed the
 * next time a build does have artifacts. The skip reason names the cause so a red
 * run is not mistaken for a broken picker.
 */
async function skipWithoutPublishedRelease(page: Page) {
  await page.goto("/downloads/");
  const hasPicker = await page.locator("[data-platform-picker]").count();
  test.skip(hasPicker === 0, "no release is published, so the page renders the unavailable notice");
}

/* The expected digest used to be read from the site's own release-index.json and
   compared against the page. That artifact is gone — release data now comes from
   GitHub Releases at build time — so there is no second copy to cross-check
   against, and asserting the shape of what the page prints is what remains. A
   64-hex digest and a real download link are still worth gating: an empty or
   truncated checksum is exactly what a reader cannot verify with. */
test("download center recommends an available artifact but keeps manual choices", async ({ page }) => {
  await skipWithoutPublishedRelease(page);
  const picker = page.getByRole("group", { name: "选择平台与架构" });
  await expect(picker).toBeVisible();
  const platform = (id: string) => picker.locator(`input[type="radio"][value="${id}"]`);
  await platform("windows-x64").check();
  await expect(page.getByRole("heading", { name: "这个平台尚未公开发行" })).toBeVisible();
  await platform("macos-arm64").check();
  await expect(page.getByRole("link", { name: "下载 macOS 预览版" })).toBeVisible();
  await expect(page.locator("[data-release-panel].is-active .hash-value")).toHaveText(/^[a-f0-9]{64}$/);
  await expect(page.getByText("未签名、未公证", { exact: true })).toBeVisible();
});

/* Security and enterprise came out of the chrome, but the pages did not go
   anywhere. The security page is where the release evidence, the privacy
   statement and the Stable gate are written down, and the footer is now the only
   route to it, so removing the nav entry and keeping the page reachable are
   asserted together — otherwise a later cleanup drops the page and the trust
   claims leave with it. The #privacy and #release-evidence anchors are checked
   because other documents cite them directly, not just the page. */
test("security and enterprise leave the navigation but stay reachable", async ({ page }) => {
  for (const path of ["/", "/en/"]) {
    await page.goto(path);
    await expect(page.locator(".site-header").getByRole("link", { name: /安全|Security/ })).toHaveCount(0);
    await expect(page.locator(".site-header").getByRole("link", { name: /企业服务|Enterprise/ })).toHaveCount(0);
    await expect(page.locator(".site-footer").getByRole("link", { name: /安全与隐私|Security & privacy/ })).toHaveCount(0);
    await expect(page.locator(".site-footer").getByRole("link", { name: /企业服务|Enterprise/ })).toHaveCount(0);
    // The footer's other two entries are the only route to them, so they stay.
    await expect(page.locator(".site-footer").getByRole("link", { name: /支持与反馈|Support & feedback/ })).toBeVisible();
    await expect(page.locator(".site-footer").getByRole("link", { name: "GitHub Releases" })).toBeVisible();
  }

  for (const path of ["/security/", "/en/security/", "/enterprise/"]) {
    const response = await page.goto(path);
    expect(response?.status(), `${path} must stay published`).toBe(200);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  }
  await page.goto("/security/");
  await expect(page.locator("#privacy")).toBeAttached();
  await expect(page.locator("#release-evidence")).toBeAttached();
});

// The picker is a real radio group rather than a styled listbox, so arrow keys
// have to move the selection — that behaviour is the reason for the markup.
test("platform picker is keyboard operable", async ({ page }) => {
  await skipWithoutPublishedRelease(page);
  const picker = page.getByRole("group", { name: "选择平台与架构" });
  await picker.locator('input[value="macos-arm64"]').focus();
  await page.keyboard.press("ArrowDown");
  await expect(picker.locator('input[value="macos-x64"]')).toBeChecked();
  await expect(page.locator('[data-release-panel="macos-x64"]')).toHaveClass(/is-active/);
  await page.keyboard.press("ArrowUp");
  await expect(picker.locator('input[value="macos-arm64"]')).toBeChecked();
  await expect(page.getByRole("link", { name: "下载 macOS 预览版" })).toBeVisible();
  // The ring belongs to the card, not the 16px dot inside it.
  await expect(picker.locator('input[value="macos-arm64"]')).toHaveCSS("outline-style", "none");
});

// The detection note is a server-rendered localised template that the client
// fills in from the DOM. Reading those templates off the wrong element leaves
// every branch as an empty string, which blanks the note instead of failing
// loudly — so both locales assert real text with no placeholder left behind.
for (const [route, expected] of [
  ["/downloads/", "已识别为"],
  ["/en/downloads/", "Detected as"],
] as const) {
  test(`the download page explains its platform detection in the page's language (${route})`, async ({ page }) => {
    await page.goto(route);
    const note = page.locator("[data-detected-note]");
    // The note lives inside the platform picker, which is absent when no release
    // is published. Same reason as skipWithoutPublishedRelease above.
    test.skip((await note.count()) === 0, "no release is published, so the page renders the unavailable notice");
    await expect(note).toContainText(expected);
    await expect(note).not.toContainText("{");
  });
}

test("guide-only compatibility remains distinct from managed installation", async ({ page }) => {
  await page.goto("/agents/cursor/");
  await expect(page.getByText("按官方方式安装", { exact: true })).toBeVisible();
  await expect(page.getByText("由 Agent 官方流程管理", { exact: true })).toBeVisible();
  await expect(page.getByText("OneAgent 可管理安装", { exact: true })).toHaveCount(0);
});

/* Replaces a test that fetched the site's own release-index.json and asserted the
   artifact it published. The site no longer publishes that file, so the guarantee
   worth keeping is the one a reader depends on: the download page states the
   channel honestly, prints a digest they can check, and sends them to the
   official release rather than a mirror the site invented. */
test("download page states the channel and links to the official release", async ({ page }) => {
  await skipWithoutPublishedRelease(page);
  /* Scoped to the active panel throughout. All four platform panels are in the
     DOM and only CSS hides the inactive ones, so an unscoped text match resolves
     against a hidden copy first and fails on visibility. */
  const active = page.locator("[data-release-panel].is-active");
  await expect(active.getByText("未签名技术预览版", { exact: true })).toBeVisible();
  await expect(active.locator(".hash-value")).toHaveText(/^[a-f0-9]{64}$/);
  const download = active.getByRole("link", { name: /下载 .* 预览版/ });
  await expect(download).toHaveAttribute("href", /^https:\/\/github\.com\/[^/]+\/[^/]+\/releases\/download\//);
});

test("serves its own stylesheet and Agent marks rather than 404ing on them", async ({ page }) => {
  // A base path build (SITE_URL/BASE_PATH, as the Pages job uses) emits an
  // absolute <base href>, and the CSP declares base-uri 'self'. Serving such a
  // build from a different origin makes the browser reject the base tag, every
  // asset path 404s, and each page still answers 200 as unstyled HTML. Only the
  // rendered result catches that, so assert on it.
  const missing: string[] = [];
  page.on("response", (response) => {
    if (response.status() >= 400) missing.push(`${response.status()} ${new URL(response.url()).pathname}`);
  });
  await page.goto("/agents/", { waitUntil: "networkidle" });
  expect(missing, "every asset the page asks for must exist").toEqual([]);
  // A stylesheet that failed to load leaves the UA default, not this palette.
  await expect(page.locator("body")).toHaveCSS("background-color", "rgb(236, 236, 239)");
  const brokenMarks = await page.evaluate(
    () => document.images.length && [...document.images].filter((image) => !image.complete || image.naturalWidth === 0).length,
  );
  expect(brokenMarks, "Agent marks must render").toBe(0);
});

test.describe("hero particles", () => {
  const frameDigest = (path: string) =>
    `(() => { const c = document.querySelector('${path}'); const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data; let h = 0; for (let i = 0; i < d.length; i += 97) h = (h * 31 + d[i]) | 0; return h; })()`;

  test("animates by default", async ({ page }) => {
    await page.goto("/");
    const canvas = page.locator("[data-hero-particles]");
    await expect(canvas).toHaveAttribute("aria-hidden", "true");
    const first = await page.evaluate(frameDigest("[data-hero-particles]"));
    await page.waitForTimeout(400);
    expect(await page.evaluate(frameDigest("[data-hero-particles]"))).not.toBe(first);
  });

  // The CSS prefers-reduced-motion block only clamps CSS animations, so the
  // canvas has to opt out in script. Only a real reduced-motion context proves it.
  test("paints a static frame under reduced motion", async ({ browser }) => {
    const page = await browser.newPage({ reducedMotion: "reduce" });
    await page.goto("/");
    const first = await page.evaluate(frameDigest("[data-hero-particles]"));
    await page.waitForTimeout(400);
    expect(await page.evaluate(frameDigest("[data-hero-particles]"))).toBe(first);
    await page.close();
  });
});

test("ships a local-only content policy and no third-party scripts", async ({ page }) => {
  await page.goto("/");
  const policy = await page.locator('meta[http-equiv="Content-Security-Policy"]').getAttribute("content");
  expect(policy).toContain("default-src 'self'");
  expect(policy).toContain("object-src 'none'");
  expect(policy).toContain("form-action 'self'");
  await expect(page.locator('script[src^="http://"], script[src^="https://"]')).toHaveCount(0);
  await expect(page.locator('iframe, object, embed')).toHaveCount(0);
});

for (const path of criticalPages) {
  test(`has no serious accessibility violations: ${path}`, async ({ page }) => {
    await page.goto(path);
    await page.addScriptTag({ content: axe.source });
    const result = await page.evaluate(async () => {
      const axeApi = (window as typeof window & { axe: typeof axe }).axe;
      return axeApi.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    });
    const serious = result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
    expect(serious, JSON.stringify(serious, null, 2)).toEqual([]);
  });
}

// axe treats <canvas> as an image node and gives up resolving the background
// behind it, so every hero and header text node comes back "incomplete" rather
// than pass — the particle layer hides exactly the copy most worth checking.
// Dropping the decoration first is what actually gates those colours.
test("hero text contrast is proven once the decorative canvas is removed", async ({ page }) => {
  await page.goto("/");
  await page.addScriptTag({ content: axe.source });
  const result = await page.evaluate(async () => {
    document.querySelector("[data-hero-particles]")?.remove();
    const axeApi = (window as typeof window & { axe: typeof axe }).axe;
    return axeApi.run(document, { runOnly: ["color-contrast"] });
  });
  expect(result.violations, JSON.stringify(result.violations, null, 2)).toEqual([]);
  expect(result.incomplete, JSON.stringify(result.incomplete, null, 2)).toEqual([]);
});

// Navigation animates via the native cross-document path, so the proof is that
// the outgoing document reports a live transition on pageswap. Asserting on the
// CSS alone would still pass if the browser declined to run it.
// Below 920px the nav collapses into a disclosure menu, and the transition is a
// document-level behaviour that does not vary by viewport — one width proves it.
test("navigating between pages runs a cross-document view transition", async ({ page, viewport }) => {
  test.skip((viewport?.width ?? 0) < 920, "nav links are collapsed at this width");
  await page.goto("/");
  await page.evaluate(() => {
    window.addEventListener("pageswap", (event) => {
      sessionStorage.setItem("vt-ran", (event as PageSwapEvent).viewTransition ? "yes" : "no");
    });
  });
  await page.getByLabel("主导航").getByRole("link", { name: "配置", exact: true }).click();
  await expect(page).toHaveURL(/\/explore\/$/);
  expect(await page.evaluate(() => sessionStorage.getItem("vt-ran"))).toBe("yes");
  // The shared chrome opts out of the crossfade by being named on both pages.
  await expect(page.locator(".site-header")).toHaveCSS("view-transition-name", "site-header");
});

test.describe("hero entrance", () => {
  const heroParts = [".eyebrow", ".display", ".lede", ".hero-actions", ".hero-note", ".product-shot"];

  test("staggers the hero into place on first paint", async ({ page, viewport }) => {
    await page.goto("/");
    const delays = await page.evaluate(() =>
      document
        .getAnimations()
        .filter((animation) => (animation as CSSAnimation).animationName === "hero-rise")
        .map((animation) => Number((animation.effect as KeyframeEffect).getTiming().delay))
        .sort((a, b) => a - b),
    );
    if ((viewport?.width ?? 0) <= 680) {
      // Stacked layout moves the block, not the lines — see the note in global.css.
      expect(delays).toEqual([0, 180]);
    } else {
      expect(delays).toEqual([0, 70, 160, 230, 290, 360]);
    }
  });

  // The previous attempt faded these in, which dropped the largest type on the
  // site below AA for the whole animation. Auditing only the settled page would
  // not have caught it, so sample while the entrance is still running.
  test("keeps hero text readable while the entrance runs", async ({ page }) => {
    await page.goto("/");
    await page.addScriptTag({ content: axe.source });
    const worst = await page.evaluate(async (parts) => {
      document.querySelector("[data-hero-particles]")?.remove();
      for (const selector of [...parts, ".hero-copy"]) {
        const element = document.querySelector<HTMLElement>(selector);
        if (!element) continue;
        element.style.animation = "none";
        void element.offsetHeight;
        element.style.animation = "";
      }
      const axeApi = (window as typeof window & { axe: typeof axe }).axe;
      let violations = 0;
      for (let sample = 0; sample < 4; sample += 1) {
        await new Promise((resolve) => setTimeout(resolve, 90));
        const result = await axeApi.run(document, { runOnly: ["color-contrast"] });
        violations += result.violations.reduce((total, entry) => total + entry.nodes.length, 0);
      }
      return violations;
    }, heroParts);
    expect(worst, "hero copy must clear AA at every frame of the entrance").toBe(0);
  });
});

test.describe("english locale", () => {
  // Derived from the route list rather than written out, so adding a translation
  // without auditing its English page is not possible.
  const englishPages = translatedRoutes.map((route) => `/en/${route}`);

  for (const path of englishPages) {
    test(`has no serious accessibility violations: ${path}`, async ({ page }) => {
      await page.goto(path);
      await expect(page.locator("html")).toHaveAttribute("lang", "en");
      await page.addScriptTag({ content: axe.source });
      const result = await page.evaluate(async () => {
        document.querySelector("[data-hero-particles]")?.remove();
        const axeApi = (window as typeof window & { axe: typeof axe }).axe;
        return axeApi.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
      });
      const serious = result.violations.filter(
        (violation) => violation.impact === "serious" || violation.impact === "critical",
      );
      expect(serious, JSON.stringify(serious, null, 2)).toEqual([]);
    });
  }

  test("declares reciprocal hreflang alternates with an x-default", async ({ page }) => {
    for (const path of ["/", "/en/", "/explore/", "/en/explore/", "/security/", "/en/security/"]) {
      await page.goto(path);
      const codes = await page.evaluate(() =>
        [...document.querySelectorAll('link[rel="alternate"][hreflang]')].map((link) => link.getAttribute("hreflang")),
      );
      expect(new Set(codes), `alternates on ${path}`).toEqual(new Set(["zh-CN", "en", "x-default"]));
    }
  });

  // An untranslated route falls back to Chinese rather than 404ing, which is the
  // right call — but silently swapping the reader's language mid-navigation is
  // not. Every link that does it has to say so before the click.
  test("marks links that fall back to Chinese, and only on English pages", async ({ page }) => {
    await page.goto("/en/");
    /* Security and enterprise left the nav, so the specific links this used to
       name are gone. The sweep below is the real guarantee and covers whatever
       is in the nav now — including the CTA band's untranslated team-services
       link, which is where the hint has to appear on this page. */

    // Every link leaving /en/ for an untranslated route carries the hint.
    const unmarked = await page.evaluate((translated) => {
      return [...document.querySelectorAll("a[href^='/']")]
        .filter((link) => {
          const href = link.getAttribute("href") ?? "";
          if (href.startsWith("/en/") || /\.(json|txt|webmanifest)$/.test(href)) return false;
          const route = href.replace(/^\//, "");
          return !translated.includes(route);
        })
        .filter((link) => !link.querySelector(".lang-hint"))
        .map((link) => link.getAttribute("href"));
    }, translatedRoutes);
    expect(unmarked, "these links change language without saying so").toEqual([]);

    // The hint is about leaving English; a Chinese reader is already there.
    await page.goto("/");
    await expect(page.locator(".lang-hint")).toHaveCount(0);
  });

  // Switching language has to keep the reader on the page they were reading;
  // dropping them on the home page is the usual failure here.
  test("language switch stays on the equivalent page", async ({ page, viewport }) => {
    test.skip((viewport?.width ?? 0) < 920, "header actions are collapsed at this width");
    await page.goto("/downloads/");
    await page.getByRole("link", { name: "切换语言" }).click();
    await expect(page).toHaveURL(/\/en\/downloads\/$/);
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    await page.getByRole("link", { name: "Change language" }).click();
    await expect(page).toHaveURL(/\/downloads\/$/);
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
  });

  test("language switch preserves the Explorer route", async ({ page, viewport }) => {
    test.skip((viewport?.width ?? 0) < 920, "header actions are collapsed at this width");
    await page.goto("/explore/?platform=macos");
    await page.getByRole("link", { name: "切换语言" }).click();
    await expect(page).toHaveURL(/\/en\/explore\/$/);
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await page.getByRole("link", { name: "Change language" }).click();
    await expect(page).toHaveURL(/\/explore\/$/);
  });

  // Every link on an English page must resolve; an untranslated destination is
  // expected to fall back to Chinese rather than 404 under /en/.
  test("navigation from an english page never lands on a missing route", async ({ page }) => {
    const missing: string[] = [];
    page.on("response", (response) => {
      if (response.status() >= 400) missing.push(`${response.status()} ${new URL(response.url()).pathname}`);
    });
    await page.goto("/en/", { waitUntil: "networkidle" });
    const targets = await page.evaluate(() =>
      [...document.querySelectorAll<HTMLAnchorElement>("a[href]")]
        .map((anchor) => anchor.href)
        .filter((href) => href.startsWith(location.origin)),
    );
    for (const target of new Set(targets)) {
      const response = await page.request.get(target);
      expect(response.status(), `${target} from /en/`).toBeLessThan(400);
    }
    expect(missing, "assets on /en/ must all exist").toEqual([]);
  });
});

test.describe("dark scheme", () => {
  // The light palette needed five values darkened to clear AA; the dark one is a
  // second, independent set of colours over different grounds, so it needs the
  // same gate rather than an assumption that inverting is safe.
  for (const path of criticalPages) {
    test(`has no serious accessibility violations in the dark: ${path}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: "dark" });
      await page.goto(path);
      await expect(page.locator("html")).toHaveClass(/theme-dark/);
      await page.addScriptTag({ content: axe.source });
      const result = await page.evaluate(async () => {
        document.querySelector("[data-hero-particles]")?.remove();
        const axeApi = (window as typeof window & { axe: typeof axe }).axe;
        return axeApi.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
      });
      const serious = result.violations.filter(
        (violation) => violation.impact === "serious" || violation.impact === "critical",
      );
      expect(serious, JSON.stringify(serious, null, 2)).toEqual([]);
    });
  }

  // axe does not measure image contrast, so nothing above would have caught the
  // Codex and OpenCode marks going invisible: they are fill="currentColor" and an
  // <img> resolves that to black regardless of the page.
  test("keeps currentColor agent marks visible without touching brand marks", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await page.goto("/agents/");
    const filters = await page.evaluate(() =>
      [...document.querySelectorAll<HTMLImageElement>(".agent-mark-wrap img")].map((image) => ({
        file: image.src.split("/").pop() ?? "",
        monochrome: image.hasAttribute("data-monochrome"),
        filter: getComputedStyle(image).filter,
      })),
    );
    const monochrome = filters.filter((entry) => entry.monochrome);
    expect(monochrome.map((entry) => entry.file).sort()).toEqual(["codex.svg", "opencode.svg"]);
    for (const entry of monochrome) expect(entry.filter, entry.file).toBe("invert(1)");
    for (const entry of filters.filter((entry) => !entry.monochrome)) {
      expect(entry.filter, `${entry.file} carries its own colours`).toBe("none");
    }

    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/agents/");
    const light = await page.evaluate(() =>
      [...document.querySelectorAll<HTMLImageElement>(".agent-mark-wrap img")].map(
        (image) => getComputedStyle(image).filter,
      ),
    );
    expect(new Set(light), "no mark is inverted in the light scheme").toEqual(new Set(["none"]));
  });

  test("remembers an explicit choice over the system preference", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/");
    await expect(page.locator("html")).not.toHaveClass(/theme-dark/);

    const toggle = page.getByRole("button", { name: "切换深色模式" });
    await toggle.click();
    await expect(page.locator("html")).toHaveClass(/theme-dark/);
    await expect(toggle).toHaveAttribute("aria-pressed", "true");

    // The inline head script is what makes the choice survive without a flash.
    await page.reload();
    await expect(page.locator("html")).toHaveClass(/theme-dark/);
    await expect(page.getByRole("button", { name: "切换深色模式" })).toHaveAttribute("aria-pressed", "true");

    await page.getByRole("button", { name: "切换深色模式" }).click();
    await expect(page.locator("html")).not.toHaveClass(/theme-dark/);
    await page.reload();
    await expect(page.locator("html")).not.toHaveClass(/theme-dark/);
  });

  test("paints the resolved theme before first contentful paint", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await page.goto("/", { waitUntil: "commit" });
    // Sampling at commit catches a theme applied late: if the class were set by
    // the deferred module script, the light palette would paint first.
    expect(await page.evaluate(() => document.documentElement.className)).toContain("theme-dark");
  });
});

// The interactive console must share the hero copy measure and size to its own
// content at every breakpoint. A clipped wizard can visually cover the next
// section even when overflow hides the pixels.
test("the activation console sits on the same measure as the copy", async ({ page }) => {
  await page.goto("/");
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((animation) => (animation as CSSAnimation).animationName === "hero-rise")
        .map((animation) => animation.finished),
    ),
  );
  const layout = await page.evaluate(() => {
    const edges = (selector: string) => {
      const rect = document.querySelector(selector)!.getBoundingClientRect();
      return { left: Math.round(rect.left), right: Math.round(rect.right) };
    };
    const panel = document.querySelector<HTMLElement>("[data-console-window]")!;
    const rect = panel.getBoundingClientRect();
    return {
      copy: edges(".hero-copy"),
      console: edges(".product-shot"),
      contentOverflow: panel.scrollHeight - Math.round(rect.height),
      gapBelowConsole: Math.round(
        document.querySelector(".hero + .section")!.getBoundingClientRect().top -
          document.querySelector(".product-shot")!.getBoundingClientRect().bottom,
      ),
    };
  });
  expect(layout.console).toEqual(layout.copy);
  expect(layout.contentOverflow, "the activation console must not clip its own content").toBeLessThanOrEqual(1);
  expect(layout.gapBelowConsole, "the console must not butt against the next section").toBeGreaterThan(16);
});

// Synthetic mouse events never set :hover, so the resting-vs-hovered difference
// is only observable by moving a real pointer.
test("interactive rows and cards respond to hover", async ({ page, viewport }) => {
  test.skip((viewport?.width ?? 0) < 920, "hover is not the primary input at this width");

  await page.goto("/agents/");
  const row = page.locator(".agent-row").first();
  await expect(row).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await row.hover();
  await expect(row).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await expect(row).not.toHaveCSS("box-shadow", "none");

  await page.goto("/providers/");
  const card = page.locator(".provider-card").first();
  const restingBorder = await card.evaluate((el) => getComputedStyle(el).borderColor);
  await card.hover();
  await expect(card).not.toHaveCSS("border-color", restingBorder);
  await expect(card).not.toHaveCSS("transform", "none");
});
