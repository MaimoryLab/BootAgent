import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { I18nProvider, LOCALE_STORAGE_KEY } from "../i18n";
import { ThemePicker } from "../components/ThemePicker";
import { THEME_STORAGE_KEY, ThemeProvider, storedPreference } from "./ThemeContext";

/** matchMedia is absent in jsdom; each test declares what the desktop reports. */
function stubSystem(dark: boolean) {
  const listeners = new Set<(event: MediaQueryListEvent) => void>();
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: dark,
      addEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.add(listener),
      removeEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.delete(listener),
    })),
  );
  return (next: boolean) => listeners.forEach((listener) => listener({ matches: next } as MediaQueryListEvent));
}

function mount() {
  render(
    <ThemeProvider>
      <I18nProvider>
        <ThemePicker />
      </I18nProvider>
    </ThemeProvider>,
  );
  return screen.getByRole("combobox", { name: "外观" });
}

/**
 * Opens the picker and clicks an option.
 *
 * Replaces userEvent.selectOptions, which only drives a native <select>. The
 * picker is now a custom listbox, so these tests go through the same two steps a
 * user does -- which is also what makes them cover the component's open/commit
 * path rather than just the provider's reducer.
 */
async function choose(trigger: HTMLElement, label: string) {
  await userEvent.click(trigger);
  await userEvent.click(screen.getByRole("option", { name: label }));
}

const classes = () => document.documentElement.className;

beforeEach(() => {
  localStorage.clear();
  // jsdom reports navigator.language as en-US, so pin the locale rather than
  // assert against whichever one the host implies.
  localStorage.setItem(LOCALE_STORAGE_KEY, "zh-CN");
  document.documentElement.className = "";
  vi.unstubAllGlobals();
});

describe("ThemeProvider", () => {
  it("carries no class while following the system", () => {
    // No class is what lets the media query in tokens.css decide. A class for
    // "system" would pin the palette and defeat the whole point.
    stubSystem(true);
    mount();
    expect(classes()).toBe("");
  });

  it("forces a palette independently of the desktop", async () => {
    stubSystem(true);
    const select = mount();
    await choose(select, "浅色");
    // theme-light on a dark desktop is the case that needs the :not() in the
    // media query, otherwise the dark block still wins.
    expect(document.documentElement.classList.contains("theme-light")).toBe(true);
    expect(document.documentElement.classList.contains("theme-dark")).toBe(false);
  });

  it("persists the choice", async () => {
    stubSystem(false);
    await choose(mount(), "深色");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(storedPreference()).toBe("dark");
  });

  it("follows a live system change only while set to system", () => {
    const flip = stubSystem(false);
    mount();
    flip(true);
    // Still no class: "system" delegates to CSS rather than mirroring the state.
    expect(classes()).toBe("");
  });

  it("keeps an explicit choice when the desktop flips", async () => {
    const flip = stubSystem(false);
    await choose(mount(), "浅色");
    flip(true);
    expect(document.documentElement.classList.contains("theme-light")).toBe(true);
  });

  it("returns to the system palette when asked", async () => {
    stubSystem(true);
    const select = mount();
    await choose(select, "深色");
    await choose(select, "跟随系统");
    expect(classes()).toBe("");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
  });

  it("overrides the media-driven window colour", async () => {
    // index.html's two theme-color tags are media-driven and cannot see a forced
    // palette, so the window chrome would keep the desktop's colour.
    stubSystem(false);
    await choose(mount(), "深色");
    // --page-bg on the dark theme. Update alongside tokens.css and index.html.
    expect(document.head.querySelector<HTMLMetaElement>("meta#theme-color-resolved")?.content).toBe("#161410");
  });

  it("treats a barred storage as no stored choice", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    // Hardened webviews reject storage; the app must still render.
    expect(storedPreference()).toBe("system");
    vi.restoreAllMocks();
  });

  it("ignores a stored value that is not a preference", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "chartreuse");
    expect(storedPreference()).toBe("system");
  });
});

describe("tokens.css", () => {
  it("declares the same dark variables for the media query and the class", () => {
    // CSS cannot share a declaration list between a media query and a class, so
    // the palette is written twice. This is the guard against the two drifting.
    const css = readFileSync(join(process.cwd(), "src/styles/tokens.css"), "utf8");
    const blocks = [...css.matchAll(/:root(?::not\(\.theme-light\)|\.theme-dark)\s*\{([^}]*)\}/g)];
    expect(blocks).toHaveLength(2);
    const variables = blocks.map((block) =>
      [...block[1].matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((match) => `${match[1]}:${match[2].trim()}`).sort(),
    );
    expect(variables[0]).toEqual(variables[1]);
  });

  it("keeps sizing tokens out of the dark blocks", () => {
    // Repeating them there would drop them whenever light is forced.
    const css = readFileSync(join(process.cwd(), "src/styles/tokens.css"), "utf8");
    for (const block of css.matchAll(/:root(?::not\(\.theme-light\)|\.theme-dark)\s*\{([^}]*)\}/g)) {
      expect(block[1]).not.toMatch(/--radius-|--sidebar-width|--footer-height/);
    }
  });
});
