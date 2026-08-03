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
  installMissingAgents: boolean;
};

/** The real ranks, so a regression in ordering shows up as the real symptom. */
const CATALOG: AgentCatalogItem[] = [
  { rank: 1, id: "codex", name: "Codex", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 2, id: "claude-code", name: "Claude Code", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 3, id: "cursor", name: "Cursor", group: "platform", configMode: "guide", guideOnly: true },
  { rank: 4, id: "opencode", name: "OpenCode", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 5, id: "openclaw", name: "OpenClaw", group: "gateway", configMode: "guide", guideOnly: true },
  { rank: 6, id: "hermes", name: "Hermes", group: "gateway", configMode: "guide", guideOnly: true },
  { rank: 8, id: "kilo-cli", name: "Kilo CLI", group: "auto", configMode: "auto", guideOnly: false },
  { rank: 9, id: "aider", name: "Aider", group: "auto", configMode: "auto", guideOnly: false },
].map((item) => ({
  ...item,
  group: item.group as AgentCatalogItem["group"],
  configMode: item.configMode as AgentCatalogItem["configMode"],
  lockedVersion: item.guideOnly ? null : "1.0.0",
  protocol: item.guideOnly ? null : ("openai" as const),
  platforms: ["macos" as const],
  platformNote: "",
}));

function renderPage() {
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
      profiles: [],
      activeProfile: null,
      firstRun: false,
    } as unknown as StatusResponse,
    statusState: "success",
    statusError: "",
    selectedAgentIds: [],
    installMissingAgents: false,
  };
  render(
    <MemoryRouter>
      <AgentSelectionPage />
    </MemoryRouter>,
  );
}

describe("AgentSelectionPage", () => {
  it("leads with the most widely used Agents, not the configurable ones", () => {
    // Grouping by catalog group put Kilo (rank 8) and Aider (9) on the first
    // screen while Cursor (3) and OpenClaw (5) sat inside a disclosure.
    renderPage();
    const names = screen.getAllByRole("radio").map((box) => box.getAttribute("aria-label"));
    expect(names).toEqual([
      "选择 Codex",
      "选择 Claude Code",
      "选择 Cursor",
      "选择 OpenCode",
      "选择 OpenClaw",
      "选择 Hermes",
    ]);
    expect(names).not.toContain("选择 Kilo CLI");
  });

  it("keeps guide-only Agents selectable", () => {
    // install_many answers a guide-only Agent with a guide-only result and
    // writes nothing. Disabling the row would break the Review flow; the
    // compliance rule is "no automatic configuration", not "no selecting".
    renderPage();
    const cursor = screen.getByLabelText("选择 Cursor");
    expect(cursor.hasAttribute("disabled")).toBe(false);
    // "仅引导" is the badge for a guide-only Agent that is not installed; once it
    // is installed the row reports 已安装 instead. Either way the copy has to say
    // this one only gets official steps.
    expect(screen.getAllByText("显示官方安装与配置步骤").length).toBe(3);
  });

  it("does not promise one-click configuration for the whole first screen", () => {
    // The first screen now mixes both kinds, so the old "可一键配置" heading
    // would be claiming something untrue of Cursor, OpenClaw and Hermes.
    renderPage();
    expect(screen.getByRole("heading", { name: "常用 Agent" })).toBeTruthy();
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

  it("counts the Agents left behind the disclosure", () => {
    renderPage();
    expect(screen.getByRole("button", { expanded: false }).textContent).toContain("2");
  });
});
