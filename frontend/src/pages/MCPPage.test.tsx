import { describe, expect, it } from "vitest";

import { changeMCPTransport, formatStdioCommandLine, mcpRowPending, parseStdioCommandLine, previewMCPForm } from "./MCPPage";

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
    expect(changeMCPTransport({ type: "stdio", command: "npx", args: ["server"], url: "old" }, "http")).toMatchObject({ type: "http", command: undefined, args: undefined, cwd: undefined });
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
