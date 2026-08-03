import { chromium } from "@playwright/test";
const base = "http://127.0.0.1:34123";
const out = [];
const b = await chromium.launch();
const page = await b.newPage({ locale: "zh-CN" });
page.on("pageerror", (e) => out.push("PAGE ERROR: " + e.message));
page.on("console", (m) => { if (m.type() === "error") out.push("CONSOLE: " + m.text()); });

await page.goto(base + "/#/overview");
await page.waitForLoadState("networkidle");
await page.getByRole("heading", { name: "运行时" }).waitFor({ timeout: 20000 });

out.push("collapsed hint: " + (await page.locator(".advanced-hint").last().innerText()).trim());
await page.getByRole("button", { name: /运行时下载/ }).click();
const sw = page.getByRole("switch");
await sw.waitFor({ timeout: 5000 });
out.push("switch: checked=" + (await sw.isChecked()));
out.push("body hint: " + (await page.locator(".advanced-body .toggle-row small").innerText()).trim());
await b.close();
console.log(out.join("\n"));
