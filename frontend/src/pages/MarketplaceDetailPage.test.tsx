import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

const { openMarketplaceExternal } = vi.hoisted(() => ({
  openMarketplaceExternal: vi.fn(() => Promise.resolve()),
}));

vi.mock("../backend/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../backend/api")>();
  return {
    ...original,
    api: { ...original.api, openMarketplaceExternal },
  };
});

vi.mock("../data/useMarketplaceCatalog", () => ({
  useMarketplaceCatalog: () => ({
    items: [{
      id: "skillhub-example-skill",
      category: "skill",
      categories: ["skill", "plugin"],
      type: "installable",
      installableKind: "skill",
      name: "Example Skill",
      description: "示例 Skill",
      icon: "Zap",
      iconColor: "oklch(62% 0.18 250)",
      source: "skillhub",
      sourceLabel: "SkillHub",
      sourceUrl: "https://skillhub.cn/skills/example-skill",
      requiresApiKey: false,
      installPrompt: "install example",
      stars: 1200,
      downloads: 3400,
    }],
  }),
}));

vi.mock("../components/SkillhubDetailSection", () => ({
  SkillhubDetailSection: () => <div data-testid="skillhub-meta">SkillHub meta</div>,
}));

vi.mock("../components/ReadmeSection", () => ({
  ReadmeSection: () => <div data-testid="skillhub-readme">README</div>,
}));

import { I18nProvider } from "../i18n";
import { MarketplaceDetailPage } from "./MarketplaceDetailPage";

function renderPage(initialEntry: string | { pathname: string; state?: unknown } = "/marketplace/skillhub-example-skill") {
  render(
    <I18nProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/marketplace/:itemId" element={<MarketplaceDetailPage />} />
          <Route path="/skills" element={<div>Skills management</div>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

beforeEach(() => openMarketplaceExternal.mockClear());

describe("MarketplaceDetailPage", () => {
  it("keeps popularity stats only in the hero and moves compact SkillHub metadata to the sidebar", () => {
    renderPage();

    expect(screen.getAllByText("1.2k")).toHaveLength(1);
    expect(screen.getAllByText("3.4k")).toHaveLength(1);
    expect(screen.queryByText("收藏数")).toBeNull();
    expect(screen.queryByText("下载量")).toBeNull();

    const sidebar = document.querySelector(".detail-meta-sidebar");
    expect(sidebar).not.toBeNull();
    expect(within(sidebar as HTMLElement).getByTestId("skillhub-meta")).toBeTruthy();
    expect(document.querySelector(".detail-main")?.querySelector("[data-testid='skillhub-meta']")).toBeNull();
    expect(screen.getByTestId("skillhub-readme")).toBeTruthy();
    expect(within(sidebar as HTMLElement).getByText("Skill")).toBeTruthy();
    expect(within(sidebar as HTMLElement).getByText("Plugins")).toBeTruthy();
  });

  it("opens source links through the desktop browser binding", () => {
    renderPage();

    fireEvent.click(screen.getByRole("link", { name: /SkillHub/ }));
    expect(openMarketplaceExternal).toHaveBeenCalledWith("https://skillhub.cn/skills/example-skill");
  });

  it("links an installed Skill back to local Skills management", () => {
    renderPage();

    fireEvent.click(screen.getByRole("link", { name: "After installation, manage it on the Skills page." }));
    expect(screen.getByText("Skills management")).toBeTruthy();
  });

  it("renders a dynamically loaded card carried in navigation state", () => {
    renderPage({
      pathname: "/marketplace/skillhub-late-page",
      state: {
        returnTo: "/marketplace?q=late",
        item: {
          id: "skillhub-late-page",
          category: "skill",
          type: "installable",
          installableKind: "skill",
          name: "Late page skill",
          description: "来自后续动态分页的 Skill",
          icon: "Puzzle",
          iconColor: "oklch(60% 0.16 75)",
          source: "skillhub",
          sourceLabel: "SkillHub",
          sourceUrl: "https://skillhub.cloud.tencent.com/skills/late-page",
        },
      },
    });

    expect(screen.getByRole("heading", { name: "Late page skill" })).toBeTruthy();
  });
});
