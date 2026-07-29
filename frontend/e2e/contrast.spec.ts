import { expect, test } from "@playwright/test";

/**
 * Text contrast, measured in the browser rather than judged by eye.
 *
 * A real-browser review found four colour tokens below WCAG AA at the sizes they
 * are actually used: --text-tertiary reached 3.0:1 as 11px secondary text, --blue
 * 4.0:1 as link text, and --green 4.0:1 inside the "已安装" badge. All three read
 * as deliberate design values, which is why nothing caught them -- the failure is
 * only visible once the ratio is computed against the colour that is really
 * painted.
 *
 * The composition step matters: reading an element's own backgroundColor reports
 * the raw rgba of a translucent chip, a colour that never appears on screen. The
 * first version of this measurement reported a failure on the segmented control
 * for exactly that reason, and the chip is in fact 14.9:1.
 */

const AA_NORMAL = 4.5;
const AA_LARGE = 3.0;

interface Sample {
  text: string;
  color: string;
  background: string;
  fontSize: number;
  fontWeight: number;
}

function channelLuminance(value: number): number {
  const scaled = value / 255;
  return scaled <= 0.03928 ? scaled / 12.92 : Math.pow((scaled + 0.055) / 1.055, 2.4);
}

function relativeLuminance([red, green, blue]: number[]): number {
  return (
    0.2126 * channelLuminance(red) +
    0.7152 * channelLuminance(green) +
    0.0722 * channelLuminance(blue)
  );
}

function parseRGB(value: string): number[] | null {
  const match = value.match(/(\d+),\s*(\d+),\s*(\d+)/);
  return match ? [Number(match[1]), Number(match[2]), Number(match[3])] : null;
}

function contrastRatio(foreground: number[], background: number[]): number {
  const first = relativeLuminance(foreground);
  const second = relativeLuminance(background);
  const [lighter, darker] = first > second ? [first, second] : [second, first];
  return (lighter + 0.05) / (darker + 0.05);
}

/** Collect one sample per distinct colour/background/size combination. */
async function collectSamples(page: import("@playwright/test").Page): Promise<Sample[]> {
  return page.evaluate(() => {
    const samples: Sample[] = [];
    const seen = new Set<string>();

    for (const element of Array.from(document.querySelectorAll<HTMLElement>("*"))) {
      // Only elements whose whole content is one text node, so the measured
      // colour is the colour that text is painted in.
      const isLeafText =
        element.childNodes.length === 1 && element.firstChild?.nodeType === Node.TEXT_NODE;
      if (!isLeafText) continue;
      const text = element.innerText.trim();
      if (text.length < 2) continue;

      const style = getComputedStyle(element);
      if (style.visibility === "hidden" || style.display === "none") continue;
      if (Number(style.opacity) === 0) continue;

      // Composite every layer down to an opaque one.
      const layers: { rgb: number[]; alpha: number }[] = [];
      for (let node: HTMLElement | null = element; node; node = node.parentElement) {
        const colour = getComputedStyle(node).backgroundColor;
        const match = colour.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/);
        if (!match) continue;
        const alpha = match[4] === undefined ? 1 : Number.parseFloat(match[4]);
        if (alpha === 0) continue;
        layers.push({ rgb: [Number(match[1]), Number(match[2]), Number(match[3])], alpha });
        if (alpha === 1) break;
      }
      let composed = [255, 255, 255];
      for (let index = layers.length - 1; index >= 0; index -= 1) {
        const { rgb, alpha } = layers[index];
        composed = composed.map((base, channel) => Math.round(rgb[channel] * alpha + base * (1 - alpha)));
      }
      const background = `rgb(${composed.join(", ")})`;

      const key = `${style.color}|${background}|${style.fontSize}|${style.fontWeight}`;
      if (seen.has(key)) continue;
      seen.add(key);

      samples.push({
        text: text.slice(0, 40),
        color: style.color,
        background,
        fontSize: Number.parseFloat(style.fontSize),
        fontWeight: Number.parseInt(style.fontWeight, 10) || 400,
      });
    }
    return samples;
  });
}

for (const [name, path] of [
  ["总览", "/#/overview"],
  ["Agent 选择", "/#/agents"],
  ["Provider", "/#/providers"],
  ["配置模板", "/#/profiles"],
] as const) {
  test(`${name}页的文字对比度达到 WCAG AA`, async ({ page }) => {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    // The status request populates most of the visible text.
    await expect(page.locator("h1, h2").first()).toBeVisible();

    const samples = await collectSamples(page);
    expect(samples.length, "no text was measured, so the assertion would be vacuous").toBeGreaterThan(3);

    const failures: string[] = [];
    for (const sample of samples) {
      const foreground = parseRGB(sample.color);
      const background = parseRGB(sample.background);
      if (!foreground || !background) continue;

      const isLarge = sample.fontSize >= 18.66 || (sample.fontSize >= 14 && sample.fontWeight >= 700);
      const required = isLarge ? AA_LARGE : AA_NORMAL;
      const ratio = contrastRatio(foreground, background);
      if (ratio < required) {
        failures.push(
          `${ratio.toFixed(2)}:1 (needs ${required}) at ${sample.fontSize}px — ` +
            `"${sample.text}" ${sample.color} on ${sample.background}`,
        );
      }
    }

    expect(failures, `text below WCAG AA:\n${failures.join("\n")}`).toEqual([]);
  });
}

test("状态色在各自的浅色背景上也达到 AA", async ({ page }) => {
  // The badges are the case the page sweep can miss: a state that needs a
  // particular Agent condition to render may not be on screen during a test run,
  // so the tokens are checked directly.
  await page.goto("/#/overview");
  await page.waitForLoadState("networkidle");

  const pairs = await page.evaluate(() => {
    const root = getComputedStyle(document.documentElement);
    const read = (name: string) => root.getPropertyValue(name).trim();
    return [
      { name: "--green on --green-soft", fg: read("--green"), bg: read("--green-soft") },
      { name: "--orange on --orange-soft", fg: read("--orange"), bg: read("--orange-soft") },
      { name: "--red on --red-soft", fg: read("--red"), bg: read("--red-soft") },
      { name: "--blue on --window-bg", fg: read("--blue"), bg: read("--window-bg") },
      { name: "--text-tertiary on --window-bg", fg: read("--text-tertiary"), bg: read("--window-bg") },
      { name: "--text-tertiary on --surface-subtle", fg: read("--text-tertiary"), bg: read("--surface-subtle") },
    ];
  });

  // Resolve each token to rgb through the browser, so a hex or an rgba both work.
  const failures: string[] = [];
  for (const pair of pairs) {
    const resolved = await page.evaluate(({ fg, bg }) => {
      const probe = document.createElement("span");
      document.body.appendChild(probe);
      probe.style.color = fg;
      const colour = getComputedStyle(probe).color;
      probe.style.color = bg;
      const background = getComputedStyle(probe).color;
      probe.remove();
      return { colour, background };
    }, pair);

    const foreground = parseRGB(resolved.colour);
    // A translucent soft background composites over the window background.
    const backgroundMatch = resolved.background.match(
      /rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?\)/,
    );
    if (!foreground || !backgroundMatch) continue;
    const alpha = backgroundMatch[4] === undefined ? 1 : Number.parseFloat(backgroundMatch[4]);
    const background = [1, 2, 3].map((channel) =>
      Math.round(Number(backgroundMatch[channel]) * alpha + 255 * (1 - alpha)),
    );

    const ratio = contrastRatio(foreground, background);
    if (ratio < AA_NORMAL) {
      failures.push(`${pair.name}: ${ratio.toFixed(2)}:1 (needs ${AA_NORMAL})`);
    }
  }

  expect(failures, `status colours below WCAG AA:\n${failures.join("\n")}`).toEqual([]);
});
