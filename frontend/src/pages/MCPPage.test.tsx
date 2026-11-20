import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));
vi.mock("../backend/api", () => ({
  api: {
    listMCP: vi.fn(() => Promise.resolve([])),
    scanMCP: vi.fn(() => Promise.resolve({ servers: [], eligible_agents: [] })),
    setMCPDraftState: vi.fn(() => Promise.resolve()),
    getMCP: vi.fn(),
    applyMCP: vi.fn(),
  },
}));
vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: { catalog: [{ id: "codex", name: "Codex" }] } } }),
}));

import { api } from "../backend/api";
import { BootAgentApiError } from "../backend/errors";
import { I18nProvider } from "../i18n";
import { FeedbackProvider } from "../state/FeedbackContext";
import { changeMCPTransport, filterMCPRows, formatStdioCommandLine, isMCPDraftComplete, MCPPage, mcpRowPending, normalizeAdvancedSpec, parseAdvancedServerID, parseAdvancedSpecJSON, parseStdioCommandLine, previewMCPForm } from "./MCPPage";
import type { MCPServerSummary } from "../types/api";

describe("stdio command line conversion", () => {
  it("uses the first whitespace-delimited token as command", () => {
    expect(parseStdioCommandLine("  npx   -y @scope/server  ")).toEqual({ command: "npx", args: ["-y", "@scope/server"] });
  });

  it("restores command and args with spaces", () => {
    expect(formatStdioCommandLine({ command: "npx", args: ["-y", "@scope/server"] })).toBe("npx -y @scope/server");
  });

  it("keeps the JSON preview in sync with the command input", () => {
    expect(previewMCPForm({ type: "stdio", env: { MODE: "dev" } }, "node server.js")).toMatchObject({ command: "node", args: ["server.js"], env: { MODE: "dev" } });
  });

  it("removes stdio fields when changing to a remote transport", () => {
    expect(changeMCPTransport({ type: "stdio", command: "npx", args: ["server"], url: "old", env: { TOKEN: "secret" } }, "http")).toMatchObject({ type: "http", command: undefined, args: undefined, cwd: undefined, env: undefined });
  });

  it("infers http when pasted JSON has a URL but no type", () => {
    expect(normalizeAdvancedSpec({ url: "https://example.test", headers: { Authorization: "Bearer key" } })).toMatchObject({ type: "http", url: "https://example.test", headers: { Authorization: "Bearer key" }, command: undefined, env: undefined });
  });

  it("accepts compact pasted remote JSON missing the final outer brace", () => {
    expect(parseAdvancedSpecJSON('{ "url": "https://example.test/mcp", "headers": { "Authorization": "Bearer key" }')).toMatchObject({ type: "http", url: "https://example.test/mcp" });
  });

  it("unwraps the common mcpServers provider format", () => {
    const value = JSON.stringify({ mcpServers: { "bing-cn-search": { url: "https://example.test/mcp", headers: { Authorization: "Bearer key" } } } });
    expect(parseAdvancedSpecJSON(value)).toMatchObject({ type: "http", url: "https://example.test/mcp", headers: { Authorization: "Bearer key" } });
    expect(parseAdvancedServerID(value)).toBe("bing-cn-search");
  });

  it("requires id, transport, and command or URL before saving", () => {
    expect(isMCPDraftComplete("server", { type: "stdio" }, "node server.js")).toBe(true);
    expect(isMCPDraftComplete("", { type: "stdio" }, "node")).toBe(false);
    expect(isMCPDraftComplete("server", { type: "" }, "node")).toBe(false);
    expect(isMCPDraftComplete("server", { type: "http" }, "")).toBe(false);
    expect(isMCPDraftComplete("server", { type: "sse" }, "")).toBe(false);
  });
});

describe("MCP row status", () => {
  it("marks only the changed server as pending", () => {
    expect(mcpRowPending("changed", { changed: { type: "stdio" } }, {})).toBe(true);
    expect(mcpRowPending("unchanged", { changed: { type: "stdio" } }, {})).toBe(false);
  });

  it("marks a row pending when only its targets changed", () => {
    expect(mcpRowPending("changed", {}, { changed: ["codex"] })).toBe(true);
    expect(mcpRowPending("unchanged", {}, { changed: ["codex"] })).toBe(false);
  });
});

describe("filterMCPRows", () => {
  const row = (id: string, type: string): MCPServerSummary => ({ id, type, agents: [], variants: 1, conflict: false, has_secrets: false });
  const rows = [row("codegraph", "stdio"), row("bing-search", "http")];

  it("returns everything for a blank query", () => {
    expect(filterMCPRows(rows, "   ")).toHaveLength(2);
  });

  it("matches id and transport case-insensitively", () => {
    expect(filterMCPRows(rows, "CODE")).toEqual([rows[0]]);
    expect(filterMCPRows(rows, "http")).toEqual([rows[1]]);
    expect(filterMCPRows(rows, "nope")).toEqual([]);
  });
});

describe("MCP marketplace discovery", () => {
  it("links both the page actions and empty state to the MCP marketplace category", () => {
    render(
      <I18nProvider>
        <MemoryRouter>
          <MCPPage />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(screen.getByRole("link", { name: "Discover in Marketplace" }).getAttribute("href")).toBe("/marketplace?category=mcp-server");
    expect(screen.getByRole("link", { name: "Or discover one in Marketplace" }).getAttribute("href")).toBe("/marketplace?category=mcp-server");
  });
});

describe("MCP page feedback", () => {
  const server: MCPServerSummary = { id: "search", type: "http", agents: ["codex"], variants: 1, conflict: false, has_secrets: false };
  const renderPage = () =>
    render(
      <I18nProvider>
        <FeedbackProvider>
          <MemoryRouter>
            <MCPPage />
          </MemoryRouter>
        </FeedbackProvider>
      </I18nProvider>,
    );

  beforeEach(() => {
    localStorage.setItem("bootagent.locale", "zh-CN");
    vi.mocked(api.listMCP).mockResolvedValue([]);
    vi.mocked(api.scanMCP).mockReset().mockResolvedValue({ servers: [server], eligible_agents: ["codex"] });
  });

  afterEach(() => {
    localStorage.clear();
    vi.mocked(api.scanMCP).mockReset().mockResolvedValue({ servers: [], eligible_agents: [] });
    vi.mocked(api.applyMCP).mockReset();
    vi.mocked(api.getMCP).mockReset();
  });

  it("tells a failed read apart from an empty registry and offers a retry", async () => {
    vi.mocked(api.scanMCP).mockRejectedValueOnce(new BootAgentApiError("disk", "CONFIG_WRITE_FAILED", false, 500));
    renderPage();

    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("无法扫描 MCP 配置");
    expect(screen.getByText("没能读取 MCP 服务器")).toBeInTheDocument();
    expect(screen.queryByText("尚未发现 MCP 服务器")).toBeNull();

    fireEvent.click(within(receipt).getByRole("button", { name: "重试" }));
    expect(await screen.findByText("search")).toBeInTheDocument();
    expect(api.scanMCP).toHaveBeenCalledTimes(2);
  });

  it("holds the apply button while applying and reports the Agents that failed", async () => {
    let finish: (value: Awaited<ReturnType<typeof api.applyMCP>>) => void = () => undefined;
    vi.mocked(api.applyMCP).mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    renderPage();

    fireEvent.click(await screen.findByTitle("删除"));
    const apply = screen.getByRole("button", { name: /应用更改（1）/ });
    fireEvent.click(apply);
    await waitFor(() => expect(apply).toHaveAttribute("aria-busy", "true"));
    expect(apply).toBeDisabled();

    finish({ results: [{ agent: "codex", config_updated: false, registry_updated: false, error: "Cannot apply MCP configuration" }] });
    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("1 项未能应用");
    fireEvent.click(within(receipt).getByRole("button", { name: "详情" }));
    expect(receipt).toHaveTextContent("Codex: Cannot apply MCP configuration");
    // The deletion is still pending, so the user can apply it again.
    expect(screen.getByText("待删除")).toBeInTheDocument();
  });

  it("confirms a successful apply", async () => {
    vi.mocked(api.applyMCP).mockResolvedValueOnce({ results: [{ agent: "codex", config_updated: true, registry_updated: true }] });
    renderPage();

    fireEvent.click(await screen.findByTitle("删除"));
    fireEvent.click(screen.getByRole("button", { name: /应用更改（1）/ }));
    expect(await screen.findByText("已同步 1 个 MCP 服务器")).toBeInTheDocument();
  });

  it("reports a server that cannot be opened for editing", async () => {
    vi.mocked(api.getMCP).mockRejectedValueOnce(new BootAgentApiError("gone", "INVALID_REQUEST", false, 400));
    renderPage();

    fireEvent.click(await screen.findByTitle("编辑"));
    expect(await screen.findByRole("alert")).toHaveTextContent("无法读取「search」");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps Save draft disabled until the server has an ID and a command", async () => {
    renderPage();
    await screen.findByText("search");

    fireEvent.click(screen.getByRole("button", { name: "新增" }));
    const save = screen.getByRole("button", { name: "保存草稿" });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("服务器 ID"), { target: { value: "files" } });
    fireEvent.change(screen.getByLabelText("命令"), { target: { value: "npx files-server" } });
    expect(save).toBeEnabled();
  });
});
