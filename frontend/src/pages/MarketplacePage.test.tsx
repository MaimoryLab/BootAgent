import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

// Block the real Wails runtime: importing it registers a module-level timer in
// drag.js that fires after jsdom teardown ("window is not defined" in CI).
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

const { catalogItems } = vi.hoisted(() => ({
  catalogItems: [
    {
      id: "skill-a",
      category: "agent-enhance",
      type: "installable",
      installableKind: "skill",
      icon: "Zap",
      iconColor: "oklch(62% 0.18 45)",
      name: "Ultracode Skill",
      description: "Multi-agent orchestration for Claude Code",
      tags: ["Claude Code", "并行"],
      installPrompt: "/install ultracode",
    },
    {
      id: "mcp-b",
      category: "mcp-server",
      type: "installable",
      installableKind: "mcp",
      icon: "Brain",
      iconColor: "oklch(58% 0.17 290)",
      name: "Sequential Thinking",
      description: "Structured reasoning for complex tasks",
      tags: ["MCP", "推理"],
      installPrompt: "install mcp",
    },
    {
      id: "link-c",
      category: "ecosystem",
      type: "external-link",
      icon: "Terminal",
      iconColor: "oklch(42% 0.06 250)",
      name: "Cursor",
      description: "AI editor",
      externalUrl: "https://cursor.com",
    },
  ],
}));

vi.mock("../data/useMarketplaceCatalog", () => ({
  useMarketplaceCatalog: () => ({ items: catalogItems, live: false }),
}));

import { I18nProvider } from "../i18n";
import { filterMarketplaceItems, MarketplacePage } from "./MarketplacePage";
import { EMPTY_FILTERS } from "../components/MarketplaceFilterSidebar";
import type { MarketplaceItem } from "../types/marketplace";

const ITEMS = catalogItems as MarketplaceItem[];

describe("filterMarketplaceItems", () => {
  it("returns all items when category is 'all' and query is empty", () => {
    expect(filterMarketplaceItems(ITEMS, "all", "")).toHaveLength(3);
  });

  it("filters by category", () => {
    const result = filterMarketplaceItems(ITEMS, "agent-enhance", "");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("skill-a");
  });

  it("filters by name (case-insensitive)", () => {
    const result = filterMarketplaceItems(ITEMS, "all", "ultracode");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("skill-a");
  });

  it("filters by description", () => {
    const result = filterMarketplaceItems(ITEMS, "all", "structured reasoning");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("mcp-b");
  });

  it("filters by tag", () => {
    const result = filterMarketplaceItems(ITEMS, "all", "推理");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("mcp-b");
  });

  it("combines category and query filters", () => {
    expect(filterMarketplaceItems(ITEMS, "agent-enhance", "MCP")).toHaveLength(0);
  });

  it("returns empty array when nothing matches", () => {
    expect(filterMarketplaceItems(ITEMS, "all", "no such thing xyz")).toHaveLength(0);
  });

  it("is case-insensitive for tags", () => {
    const result = filterMarketplaceItems(ITEMS, "all", "claude code");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("skill-a");
  });

  it("filters by installableKind via FilterState.kinds", () => {
    const filters = { ...EMPTY_FILTERS, kinds: new Set(["mcp" as const]) };
    const result = filterMarketplaceItems(ITEMS, "all", "", filters);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("mcp-b");
  });
});

describe("MarketplacePage category URL", () => {
  it("selects and displays the category provided by the route query", () => {
    render(
      <I18nProvider>
        <MemoryRouter initialEntries={["/marketplace?category=mcp-server"]}>
          <MarketplacePage />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(screen.getByRole("tab", { name: /MCP servers/ }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("Sequential Thinking")).toBeTruthy();
    expect(screen.queryByText("Ultracode Skill")).toBeNull();
  });
});
