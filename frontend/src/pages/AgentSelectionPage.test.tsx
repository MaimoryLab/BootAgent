import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type { AgentCatalogItem, StatusResponse } from "../types/api";
import { AgentSelectionPage } from "./AgentSelectionPage";

const dispatch = vi.fn();

vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: mockState, dispatch, refreshStatus: vi.fn() }),
}));

let mockState: {
  status: StatusResponse | null;
  statusState: string;
  statusError: string;
  selectedAgentIds: string[];
  setupKind?: string;
};

/** The real ranks, so a regression in ordering shows up as the real symptom. */
const CATALOG: AgentCatalogItem[] = [
  { rank: 1, id: "codex", name: "Codex", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 2, id: "claude-code", name: "Claude Code", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 3, id: "opencode", name: "OpenCode", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 4, id: "kilo-cli", name: "Kilo CLI", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 5, id: "aider", name: "Aider", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 6, id: "openclaw", name: "OpenClaw", group: "auto", configMode: "auto", guideOnly: false },
].map((item) => ({
  ...item,
  group: item.group as AgentCatalogItem["group"],
  configMode: item.configMode as AgentCatalogItem["configMode"],
  lockedVersion: item.guideOnly ? null : "1.0.0",
  protocol: item.guideOnly ? null : ("openai" as const),
  platforms: ["macos" as const],
  platformNote: "",
}));

/** Two desktop Agents: one with a registered image mark, one with a generic mark. */
const DESKTOP_AGENTS = [
  {
    id: "chatgpt-desktop", name: "ChatGPT Desktop", installed: false, supported: true,
    source: "unknown", version: null, profileAgentId: "codex", profileId: null, protocol: "responses",
  },
  {
    id: "zcode", name: "ZCode", installed: false, supported: true,
    source: "unknown", version: null, profileAgentId: "zcode", profileId: null, protocol: "openai",
  },
];

function renderPage({ desktop = false }: { desktop?: boolean } = {}) {
  dispatch.mockClear();
  mockState = {
    status: {
      apiVersion: 1,
      platform: { os: "macos", arch: "arm64", shell: "bash" },
      runtimes: [],
      capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
      agents: Object.fromEntries(
        CATALOG.map((item) => [
          item.id,
          {
            installed: false,
            configured: false,
            guideOnly: item.guideOnly,
            config: "/c",
            version: null,
            lockedVersion: item.lockedVersion,
            canInstall: !item.guideOnly,
            provider: null,
            profileId: null,
            model: null,
            baseUrl: null,
            updatedAt: null,
          },
        ]),
      ),
      // Deliberately not pre-sorted: the page must not depend on server order.
      catalog: [...CATALOG].reverse(),
      groups: [],
      providers: {},
      paths: {},
      backups: {},
      environment: null,
      environmentError: null,
      desktopAgents: desktop ? DESKTOP_AGENTS : [],
      profiles: [],
      activeProfile: null,
      firstRun: false,
    } as unknown as StatusResponse,
    statusState: "success",
    statusError: "",
    selectedAgentIds: [],
    // The tab is reducer state, and the reducer is mocked here, so clicking the
    // tab dispatches without re-rendering. Set it directly instead.
    setupKind: desktop ? "desktop" : "cli",
  };
  render(
    <MemoryRouter>
      <AgentSelectionPage />
    </MemoryRouter>,
  );
}

describe("AgentSelectionPage", () => {
  it("lists every Agent together in rank order", () => {
    renderPage();
    const names = screen.getAllByRole("radio").map((box) => box.getAttribute("aria-label"));
    expect(names).toEqual([
      "选择 Codex",
      "选择 Claude Code",
      "选择 OpenCode",
      "选择 Kilo CLI",
      "选择 Aider",
      "选择 OpenClaw",
    ]);
  });

  it("does not promise one-click configuration for the whole first screen", () => {
    // Every catalog Agent is one-click configurable today, but the heading is
    // still wrong to promise it: OpenClaw is configured only as far as its model
    // provider, and a guide-only Agent can return to the catalog at any time.
    renderPage();
    expect(screen.getByRole("heading", { name: "选择 Agent", level: 2 })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "可一键配置" })).toBeNull();
  });

  it("offers one Agent per run and blocks the step until one is chosen", () => {
    // Onboarding configures a single Agent end to end: radios, not checkboxes,
    // and SELECT_AGENT replaces rather than accumulates.
    renderPage();
    expect(screen.getAllByRole("radio")[0]?.getAttribute("type")).toBe("radio");
    expect(screen.queryAllByRole("checkbox").map((box) => box.getAttribute("aria-label"))).not.toContain("选择 Codex");
    expect(screen.getByRole("button", { name: /继续/ }).hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByLabelText("选择 Codex"));
    expect(dispatch).toHaveBeenCalledWith({ type: "SELECT_AGENT", agentId: "codex" });
  });

  // The desktop rows used to render one hardcoded AppWindow glyph for every
  // Agent, so they carried no data-mark-kind at all and ChatGPT Desktop lost the
  // mark it has registered. The CLI rows on the same page always used AgentIcon,
  // which is the inconsistency this asserts against.
  it("gives each desktop Agent its own mark, like the CLI rows", () => {
    renderPage({ desktop: true });
    const marks = screen.getAllByLabelText(/^选择 /).map((radio) => {
      const row = radio.closest(".agent-row");
      return {
        name: radio.getAttribute("aria-label"),
        kind: row?.querySelector("[data-mark-kind]")?.getAttribute("data-mark-kind") ?? null,
      };
    });
    // Both rows must resolve through the icon registry rather than a literal:
    // ChatGPT Desktop to a licensed vector, ZCode to Z.ai's own bitmap.
    expect(marks).toEqual([
      { name: "选择 ChatGPT Desktop", kind: "asset" },
      { name: "选择 ZCode", kind: "raster" },
    ]);
  });
});
