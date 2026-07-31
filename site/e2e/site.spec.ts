import { test, expect } from "@playwright/test";
import axe from "axe-core";

const criticalPages = ["/", "/explore/", "/downloads/", "/agents/", "/security/"];

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
  await expect(page.locator("#activation-console")).toHaveAttribute("data-phase", "idle");
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
  await expect(console.getByRole("link", { name: "打开完整 Explorer" })).toBeVisible();
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

test("download center recommends an available artifact but keeps manual choices", async ({ page, request }) => {
  const index = await (await request.get("/release-index.json")).json();
  const preview = index.channels.find((channel: { channel: string }) => channel.channel === "technical-preview-unsigned");
  const mac = preview.targets.find((target: { id: string }) => target.id === "macos-arm64");
  const expectedSha = mac.artifacts[0].sha256;
  await page.goto("/downloads/");
  const picker = page.getByRole("group", { name: "选择平台与架构" });
  await expect(picker).toBeVisible();
  const platform = (id: string) => picker.locator(`input[type="radio"][value="${id}"]`);
  await platform("windows-x64").check();
  await expect(page.getByRole("heading", { name: "这个平台尚未公开发行" })).toBeVisible();
  await platform("macos-arm64").check();
  await expect(page.getByRole("link", { name: "下载 macOS 预览版" })).toBeVisible();
  await expect(page.getByText(expectedSha, { exact: true })).toBeVisible();
  await expect(page.getByText("未签名、未公证", { exact: true })).toBeVisible();
});

// The picker is a real radio group rather than a styled listbox, so arrow keys
// have to move the selection — that behaviour is the reason for the markup.
test("platform picker is keyboard operable", async ({ page }) => {
  await page.goto("/downloads/");
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

test("public release index exposes the same verified artifact", async ({ request }) => {
  const response = await request.get("/release-index.json");
  expect(response.ok()).toBeTruthy();
  const index = await response.json();
  const preview = index.channels.find((channel: { channel: string }) => channel.channel === "technical-preview-unsigned");
  const mac = preview.targets.find((target: { id: string }) => target.id === "macos-arm64");
  expect(preview.version).toBe("0.2.0-dev");
  expect(preview.unsigned).toBe(true);
  expect(mac.verification.cleanroom).toBe("verified");
  expect(mac.artifacts[0].sha256).toMatch(/^[a-f0-9]{64}$/);
  expect(mac.artifacts[0].downloads.some((download: { kind: string; primary: boolean }) => download.kind === "official" && download.primary)).toBe(true);
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
  await page.getByLabel("主导航").getByRole("link", { name: "Explorer", exact: true }).click();
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
  const englishPages = ["/en/", "/en/explore/", "/en/downloads/", "/en/quickstart/"];

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
    for (const path of ["/", "/en/", "/explore/", "/en/explore/"]) {
      await page.goto(path);
      const codes = await page.evaluate(() =>
        [...document.querySelectorAll('link[rel="alternate"][hreflang]')].map((link) => link.getAttribute("hreflang")),
      );
      expect(new Set(codes), `alternates on ${path}`).toEqual(new Set(["zh-CN", "en", "x-default"]));
    }
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
