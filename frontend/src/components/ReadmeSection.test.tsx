import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

const { marketplaceSkillFile, openMarketplaceExternal } = vi.hoisted(() => ({
  marketplaceSkillFile: vi.fn(() => Promise.resolve("# Skill README\n\n[Docs](https://example.com/docs) · [Usage](#usage)\n\n## Usage")),
  openMarketplaceExternal: vi.fn(() => Promise.resolve()),
}));

vi.mock("../backend/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../backend/api")>();
  return {
    ...original,
    api: { ...original.api, marketplaceSkillFile, openMarketplaceExternal },
  };
});

import { I18nProvider } from "../i18n";
import { ReadmeSection } from "./ReadmeSection";

describe("ReadmeSection", () => {
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
});
