import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));
vi.mock("../backend/api", () => ({
  api: {
    listMCP: vi.fn(() => Promise.resolve([])),
    scanMCP: vi.fn(() => Promise.resolve({ servers: [], eligible_agents: [] })),
    setMCPDraftState: vi.fn(() => Promise.resolve()),
  },
}));
vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: { catalog: [] } } }),
}));

import { I18nProvider } from "../i18n";
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
