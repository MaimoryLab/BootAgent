import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Block the real Wails runtime: importing it registers a module-level timer in
// drag.js that fires after jsdom teardown ("window is not defined" in CI).
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

import { SkillhubDetailSection } from "./SkillhubDetailSection";
import { I18nProvider, LOCALE_STORAGE_KEY } from "../i18n";

const DETAIL_PAYLOAD = {
  securityReports: {
    keen: {
      status: "benign",
      statusText: "安全，无风险",
      reportUrl: "https://tix.qq.com/search/skill?keyword=abc",
    },
    sanbu: {
      status: "suspicious",
      statusText: "存在风险提示",
      reportUrl: "https://static.cloudsec.tencent.com/report.html",
    },
  },
  latestVersion: { version: "3.0.24", changelog: "Synced by skillhub pipeline", createdAt: 1782524296584 },
  owner: { displayName: "pskoett", handle: "pskoett" },
  skill: {
    stats: { downloads: 1132494, installs: 86140, stars: 4433, comments: 12, versions: 9 },
    sourceUrl: "https://clawhub.ai/pskoett/self-improving-agent",
  },
};

function mockFetchOnce(impl: () => Promise<Response>) {
  vi.stubGlobal("fetch", vi.fn(impl));
}

beforeEach(() => {
  // jsdom's navigator.language is English; pin the source locale so the
  // Chinese source-string assertions below are environment-independent.
  localStorage.setItem(LOCALE_STORAGE_KEY, "zh-CN");
});

afterEach(() => {
  localStorage.removeItem(LOCALE_STORAGE_KEY);
  vi.unstubAllGlobals();
});

describe("SkillhubDetailSection", () => {
  it("renders security verdicts, version info and author from the detail API", async () => {
    mockFetchOnce(() =>
      Promise.resolve(new Response(JSON.stringify(DETAIL_PAYLOAD), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })),
    );

    render(
      <I18nProvider>
        <SkillhubDetailSection slug="self-improving-agent" />
      </I18nProvider>,
    );

    // Security block: benign -> success badge, anything else -> warning.
    expect(await screen.findByText("安全，无风险")).toBeTruthy();
    expect(screen.getByText("安全，无风险").className).toContain("status-success");
    expect(screen.getByText("存在风险提示").className).toContain("status-warning");
    expect(screen.getByLabelText("科恩实验室 - 查看报告")).toBeTruthy();
    expect(screen.getByLabelText("三堡实验室 - 查看报告")).toBeTruthy();

    // Compact sidebar metadata: version, installs and comments.
    expect(screen.getByText("3.0.24")).toBeTruthy();
    expect(screen.getByText("86.1k")).toBeTruthy();
    expect(screen.getByText("12")).toBeTruthy();
    expect(screen.queryByText("安全审核")).toBeNull();
    expect(screen.queryByText(/Synced by skillhub pipeline/)).toBeNull();

    // Author block with the upstream link.
    expect(screen.getByText("pskoett")).toBeTruthy();
    const upstream = screen.getByLabelText("上游来源").closest("a");
    expect(upstream?.getAttribute("href")).toBe("https://clawhub.ai/pskoett/self-improving-agent");
  });

  it("renders nothing when the fetch fails (silent degradation)", async () => {
    mockFetchOnce(() => Promise.reject(new TypeError("blocked by CORS")));

    const { container } = render(
      <I18nProvider>
        <SkillhubDetailSection slug="self-improving-agent" />
      </I18nProvider>,
    );

    await waitFor(() => {
      expect(container.querySelector(".readme-loading")).toBeNull();
    });
    expect(container.innerHTML).toBe("");
  });
});
