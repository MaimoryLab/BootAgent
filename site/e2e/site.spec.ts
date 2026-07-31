import { test, expect } from "@playwright/test";
import axe from "axe-core";

const criticalPages = ["/", "/downloads/", "/agents/", "/security/"];

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

test("home states the product boundary and current channel", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("激活你的 AI 开发环境");
  await expect(page.getByRole("link", { name: "下载 OneAgent" })).toBeVisible();
  await expect(page.getByText("自己的 Key", { exact: true })).toBeVisible();
  await expect(page.locator(".hero-note")).toContainText("GitHub");
});

test("download center links only to GitHub Releases", async ({ page }) => {
  await page.goto("/downloads/");
  const releaseLink = page.getByRole("link", { name: /查看 GitHub Releases?/ }).first();
  await expect(releaseLink).toBeVisible();
  await expect(releaseLink).toHaveAttribute("href", /^https:\/\/github\.com\/MaimoryLab\/OneAgent\/releases/);
});

test("guide-only compatibility remains distinct from managed installation", async ({ page }) => {
  await page.goto("/agents/cursor/");
  await expect(page.getByText("按官方方式安装", { exact: true })).toBeVisible();
  await expect(page.getByText("由 Agent 官方流程管理", { exact: true })).toBeVisible();
  await expect(page.getByText("OneAgent 可管理安装", { exact: true })).toHaveCount(0);
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
  await expect(page.locator("body")).toHaveCSS("background-color", "rgb(244, 243, 239)");
  const brokenMarks = await page.evaluate(
    () => document.images.length && [...document.images].filter((image) => !image.complete || image.naturalWidth === 0).length,
  );
  expect(brokenMarks, "Agent marks must render").toBe(0);
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
