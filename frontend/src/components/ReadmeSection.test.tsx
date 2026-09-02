import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

const { marketplaceSkillFile, marketplaceMCPServerReadme, marketplaceMCPServersDirectoryReadme, openMarketplaceExternal } = vi.hoisted(() => ({
  marketplaceSkillFile: vi.fn(() => Promise.resolve("# Skill README\n\n[Docs](https://example.com/docs) · [Usage](#usage)\n\n## Usage")),
  marketplaceMCPServerReadme: vi.fn(() => Promise.resolve("# MCP README\n\n[Docs](https://example.com/mcp-docs)")),
  marketplaceMCPServersDirectoryReadme: vi.fn(() => Promise.resolve("# Directory README\n\n[Docs](./docs)")),
  openMarketplaceExternal: vi.fn(() => Promise.resolve()),
}));

vi.mock("../backend/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../backend/api")>();
  return {
    ...original,
    api: { ...original.api, marketplaceSkillFile, marketplaceMCPServerReadme, marketplaceMCPServersDirectoryReadme, openMarketplaceExternal },
  };
});

import { I18nProvider } from "../i18n";
import { ReadmeSection, stripSkillhubMetadataPreamble } from "./ReadmeSection";

describe("ReadmeSection", () => {
  it("removes SkillHub metadata preambles from the rendered README", () => {
    expect(stripSkillhubMetadataPreamble([
      "README",
      "name: self-improvement",
      'description: "Captures learnings"',
      "metadata:",
      "",
      "Self-Improvement Skill",
      "",
      "实际内容",
    ].join("\n"))).toBe("Self-Improvement Skill\n\n实际内容");
  });

  it("loads SkillHub SKILL.md and opens its links in the desktop browser", async () => {
    render(
      <I18nProvider>
        <ReadmeSection skillhubSlug="example-skill" />
      </I18nProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Skill README" })).toBeTruthy();
    expect(marketplaceSkillFile).toHaveBeenCalledWith("example-skill");

    fireEvent.click(screen.getByRole("link", { name: "Docs" }));
    expect(openMarketplaceExternal).toHaveBeenCalledWith("https://example.com/docs");

    openMarketplaceExternal.mockClear();
    fireEvent.click(screen.getByRole("link", { name: "Usage" }));
    expect(openMarketplaceExternal).not.toHaveBeenCalled();
  });

  it("hides SkillHub SKILL.md front matter and renders only its document body", async () => {
    marketplaceSkillFile.mockResolvedValueOnce(`---
name: self-improvement
description: "Captures learnings, errors, and corrections to enable continuous improvement."
metadata:
---

# Self-Improvement Skill

Document body.`);

    render(
      <I18nProvider>
        <ReadmeSection skillhubSlug="self-improving-agent" />
      </I18nProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Self-Improvement Skill" })).toBeTruthy();
    expect(screen.queryByText("name: self-improvement")).toBeNull();
    expect(screen.queryByText(/Captures learnings, errors/)).toBeNull();
    expect(screen.queryByText("metadata:")).toBeNull();
    expect(screen.getByText("Document body.")).toBeTruthy();
  });

  it("loads MCP Server README through the desktop binding", async () => {
    render(
      <I18nProvider>
        <ReadmeSection mcpServerSlug="demo-mcp" />
      </I18nProvider>,
    );

    expect(await screen.findByRole("heading", { name: "MCP README" })).toBeTruthy();
    expect(marketplaceMCPServerReadme).toHaveBeenCalledWith("demo-mcp");
    fireEvent.click(screen.getByRole("link", { name: "Docs" }));
    expect(openMarketplaceExternal).toHaveBeenCalledWith("https://example.com/mcp-docs");
  });

  it("loads mcpservers.org README through the directory binding and resolves relative links", async () => {
    render(
      <I18nProvider>
        <ReadmeSection mcpServersOrgPath="acme/demo-server" />
      </I18nProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Directory README" })).toBeTruthy();
    expect(marketplaceMCPServersDirectoryReadme).toHaveBeenCalledWith("acme/demo-server");
    fireEvent.click(screen.getByRole("link", { name: "Docs" }));
    expect(openMarketplaceExternal).toHaveBeenCalledWith("https://mcpservers.org/servers/acme/demo-server/docs");
  });
});
