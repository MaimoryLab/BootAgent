import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));
vi.mock("../backend/api", () => ({
  api: {
    listSkills: vi.fn(() => Promise.resolve([])),
    scanSkills: vi.fn(() => Promise.resolve({ skills: [], candidates: [], eligible_agents: [] })),
    setSkillDraftState: vi.fn(() => Promise.resolve()),
    previewSkillImport: vi.fn(),
    applySkills: vi.fn(),
  },
}));
vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: { catalog: [{ id: "codex", name: "Codex" }] } } }),
}));

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { I18nProvider } from "../i18n";
import { FeedbackProvider } from "../state/FeedbackContext";
import { applySkillTarget, filterSkillRows, skillSelectedAgents, SkillsPage } from "./SkillsPage";
import type { SkillChange, SkillSummary } from "../types/api";

const row = (partial: Partial<SkillSummary>): SkillSummary => ({
  id: "skill",
  name: "Skill",
  description: "",
  variants: 1,
  variant_hashes: ["hash"],
  agents: [],
  conflict: false,
  ...partial,
});

describe("filterSkillRows", () => {
  const rows = [
    row({ id: "dataviz", name: "DataViz", description: "chart palettes" }),
    row({ id: "pdf", name: "PDF tools", description: "merge and split" }),
  ];

  it("returns everything for a blank query", () => {
    expect(filterSkillRows(rows, "  ")).toHaveLength(2);
  });

  it("matches id, name, and description case-insensitively", () => {
    expect(filterSkillRows(rows, "DATAVIZ")).toEqual([rows[0]]);
    expect(filterSkillRows(rows, "merge")).toEqual([rows[1]]);
    expect(filterSkillRows(rows, "nothing")).toEqual([]);
  });
});

describe("skillSelectedAgents", () => {
  it("starts from the scanned agents", () => {
    expect(skillSelectedAgents(row({ agents: ["codex"] }), {})).toEqual(["codex"]);
  });

  it("prefers the draft targets once a change exists", () => {
    const changes: Record<string, SkillChange> = {
      skill: { id: "skill", variant_hash: "hash", targets: ["claude-code"] },
    };
    expect(skillSelectedAgents(row({ agents: ["codex"] }), changes)).toEqual(["claude-code"]);
  });

  it("drops agents queued for removal", () => {
    const changes: Record<string, SkillChange> = {
      "skill::remove::codex": { id: "skill", variant_hash: "hash", targets: ["codex"], delete: true },
    };
    expect(skillSelectedAgents(row({ agents: ["codex", "opencode"] }), changes)).toEqual(["opencode"]);
  });
});

describe("applySkillTarget", () => {
  const target = { id: "skill", hash: "hash", scannedAgents: ["codex"] };

  it("selects a new agent starting from the scanned set", () => {
    const next = applySkillTarget({}, target, "claude-code", true);
    expect(next["skill"].targets?.sort()).toEqual(["claude-code", "codex"]);
  });

  it("records a removal entry when deselecting a scanned agent", () => {
    const next = applySkillTarget({}, target, "codex", false);
    expect(next["skill::remove::codex"]).toMatchObject({ id: "skill", delete: true, targets: ["codex"] });
  });

  it("re-selecting a removed agent clears its removal entry", () => {
    const removed = applySkillTarget({}, target, "codex", false);
    const restored = applySkillTarget(removed, target, "codex", true);
    expect(restored["skill::remove::codex"]).toBeUndefined();
    expect(restored["skill"].targets).toContain("codex");
  });

  it("folds sequential toggles without losing earlier ones", () => {
    // The bulk toggle applies this reducer once per row inside a single
    // setState updater; two toggles on one row must both survive.
    let draft: Record<string, SkillChange> = {};
    draft = applySkillTarget(draft, target, "claude-code", true);
    draft = applySkillTarget(draft, target, "opencode", true);
    expect(draft["skill"].targets?.sort()).toEqual(["claude-code", "codex", "opencode"]);
  });
});

describe("Skills marketplace discovery", () => {
  it("links both the page actions and empty state to the Skill marketplace category", () => {
    render(
      <I18nProvider>
        <MemoryRouter>
          <SkillsPage />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(screen.getByRole("link", { name: "Discover in Marketplace" }).getAttribute("href")).toBe("/marketplace?category=agent-enhance");
    expect(screen.getByRole("link", { name: "Or discover one in Marketplace" }).getAttribute("href")).toBe("/marketplace?category=agent-enhance");
  });

  it("links to the multimodal page instead of listing it below the Skill library", () => {
    render(
      <I18nProvider>
        <MemoryRouter>
          <SkillsPage />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(screen.getByRole("link", { name: "Multimodal capabilities" }).getAttribute("href")).toBe("/multimodal");
    expect(screen.queryByRole("heading", { name: "Multimodal capabilities" })).toBeNull();
  });
});

describe("Skills page feedback", () => {
  const skill = row({ id: "pdf", name: "PDF tools", agents: ["codex"] });
  const renderPage = () =>
    render(
      <I18nProvider>
        <FeedbackProvider>
          <MemoryRouter>
            <SkillsPage />
          </MemoryRouter>
        </FeedbackProvider>
      </I18nProvider>,
    );

  beforeEach(() => {
    localStorage.setItem("bootagent.locale", "zh-CN");
    vi.mocked(api.scanSkills).mockReset().mockResolvedValue({ skills: [skill], candidates: [], eligible_agents: ["codex"] });
  });

  afterEach(() => {
    localStorage.clear();
    vi.mocked(api.scanSkills).mockReset().mockResolvedValue({ skills: [], candidates: [], eligible_agents: [] });
    vi.mocked(api.applySkills).mockReset();
    vi.mocked(api.previewSkillImport).mockReset();
  });

  it("tells a failed read apart from an empty library and offers a retry", async () => {
    vi.mocked(api.scanSkills).mockRejectedValueOnce(new BootAgentApiError("disk", "CONFIG_WRITE_FAILED", false, 500));
    renderPage();

    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("无法扫描 Skills");
    expect(screen.getByText("没能读取 Skills")).toBeInTheDocument();
    expect(screen.queryByText("没有发现 Skills")).toBeNull();

    fireEvent.click(within(receipt).getByRole("button", { name: "重试" }));
    expect(await screen.findByText("PDF tools")).toBeInTheDocument();
  });

  it("keeps a deleted Skill listed until the backend confirms the delete", async () => {
    let finish: (value: Awaited<ReturnType<typeof api.applySkills>>) => void = () => undefined;
    vi.mocked(api.applySkills).mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    renderPage();

    fireEvent.click(await screen.findByTitle("删除"));
    const apply = screen.getByRole("button", { name: /应用 Skills（1）/ });
    fireEvent.click(apply);
    await waitFor(() => expect(apply).toHaveAttribute("aria-busy", "true"));
    expect(screen.getByText("PDF tools")).toBeInTheDocument();

    finish({ results: [{ agent: "codex", target_updated: false, registry_updated: false, error: "Skill target path is not private" }] });
    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("1 项未能应用");
    fireEvent.click(within(receipt).getByRole("button", { name: "详情" }));
    expect(receipt).toHaveTextContent("Codex: Skill target path is not private");
    await waitFor(() => expect(apply).not.toHaveAttribute("aria-busy", "true"));
    expect(screen.getByText("PDF tools")).toBeInTheDocument();
    expect(screen.getByText("待删除")).toBeInTheDocument();
  });

  it("does nothing when the import picker is closed", async () => {
    vi.mocked(api.previewSkillImport).mockResolvedValueOnce({ token: "", candidates: [] });
    renderPage();
    await screen.findByText("PDF tools");

    fireEvent.click(screen.getByRole("button", { name: "导入 ZIP" }));
    await waitFor(() => expect(api.previewSkillImport).toHaveBeenCalledWith("zip"));
    await waitFor(() => expect(screen.getByRole("button", { name: "导入 ZIP" })).toBeEnabled());
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("reports an import that could not be read", async () => {
    vi.mocked(api.previewSkillImport).mockRejectedValueOnce(new BootAgentApiError("bad zip", "INVALID_REQUEST", false, 400));
    renderPage();
    await screen.findByText("PDF tools");

    fireEvent.click(screen.getByRole("button", { name: "导入 ZIP" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("无法读取要导入的 Skill");
  });

  it("confirms an import with a receipt instead of a line in the heading", async () => {
    vi.mocked(api.previewSkillImport).mockResolvedValueOnce({ token: "t", candidates: [{ id: "zip-skill", name: "Zip skill", description: "", hash: "h", source: "zip", files: 1, bytes: 1, stored: false }] });
    vi.mocked(api.applySkills).mockResolvedValueOnce({ results: [] });
    renderPage();
    await screen.findByText("PDF tools");

    fireEvent.click(screen.getByRole("button", { name: "导入 ZIP" }));
    fireEvent.click(await screen.findByRole("button", { name: "导入到 BootAgent" }));
    expect(await screen.findByText("已导入 1 个 Skill")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
