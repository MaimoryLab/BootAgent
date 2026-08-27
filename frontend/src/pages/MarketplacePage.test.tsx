import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

// Block the real Wails runtime: importing it registers a module-level timer in
// drag.js that fires after jsdom teardown ("window is not defined" in CI).
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));

const { catalogItems } = vi.hoisted(() => ({
  catalogItems: [
    {
      id: "skill-a",
      category: "skill",
      categories: ["skill", "plugin"],
      type: "installable",
      installableKind: "skill",
      icon: "Zap",
      iconColor: "oklch(62% 0.18 45)",
      name: "Ultracode Skill",
      description: "Multi-agent orchestration for Claude Code",
      tags: ["Claude Code", "并行"],
      scene: "coding",
      source: "skillhub",
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
      scene: "integration",
      source: "mcpservers",
      installPrompt: "install mcp",
    },
    {
      id: "link-c",
      category: "ai-product",
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
import { api } from "../backend/api";
import { filterMarketplaceItems, MarketplacePage, parseMarketplaceFilters, serializeMarketplaceFilters } from "./MarketplacePage";
import { EMPTY_FILTERS } from "../components/MarketplaceFilterSidebar";
import type { MarketplaceItem } from "../types/marketplace";

const ITEMS = catalogItems as MarketplaceItem[];

afterEach(() => vi.restoreAllMocks());

describe("filterMarketplaceItems", () => {
  it("returns all items when category is 'all' and query is empty", () => {
    expect(filterMarketplaceItems(ITEMS, "all", "")).toHaveLength(3);
  });

  it("filters by category", () => {
    const result = filterMarketplaceItems(ITEMS, "skill", "");
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe("skill-a");
  });

  it("includes a multi-type tool in every declared top-level type", () => {
    expect(filterMarketplaceItems(ITEMS, "skill", "").map((item) => item.id)).toEqual(["skill-a"]);
    expect(filterMarketplaceItems(ITEMS, "plugin", "").map((item) => item.id)).toEqual(["skill-a"]);
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
    expect(filterMarketplaceItems(ITEMS, "skill", "MCP")).toHaveLength(0);
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

  it("requires a multi-type tool to match every selected tool type", () => {
    const pluginFilters = { ...EMPTY_FILTERS, kinds: new Set(["plugin" as const]) };
    expect(filterMarketplaceItems(ITEMS, "all", "", pluginFilters).map((item) => item.id)).toEqual(["skill-a"]);

    const hybridFilters = { ...EMPTY_FILTERS, kinds: new Set(["plugin" as const, "skill" as const]) };
    expect(filterMarketplaceItems(ITEMS, "all", "", hybridFilters).map((item) => item.id)).toEqual(["skill-a"]);

    const incompatibleFilters = { ...EMPTY_FILTERS, kinds: new Set(["plugin" as const, "mcp" as const]) };
    expect(filterMarketplaceItems(ITEMS, "all", "", incompatibleFilters)).toEqual([]);
  });

  it("renders each tool ID at most once even when a source repeats it", () => {
    const filters = { ...EMPTY_FILTERS, kinds: new Set(["skill" as const]) };
    const duplicated = [ITEMS[0], { ...ITEMS[0] }, ITEMS[1]];
    expect(filterMarketplaceItems(duplicated, "all", "", filters).map((item) => item.id)).toEqual(["skill-a"]);
  });

  it("excludes entries missing the selected source or scene", () => {
    const sourceFilters = { ...EMPTY_FILTERS, sources: new Set(["mcpservers" as const]) };
    expect(filterMarketplaceItems(ITEMS, "all", "", sourceFilters).map((item) => item.id)).toEqual(["mcp-b"]);
    const sceneFilters = { ...EMPTY_FILTERS, scenes: new Set(["coding" as const]) };
    expect(filterMarketplaceItems(ITEMS, "all", "", sceneFilters).map((item) => item.id)).toEqual(["skill-a"]);
  });
});

describe("MarketplacePage category URL", () => {
  it("round-trips marketplace filters through the URL", () => {
    const params = serializeMarketplaceFilters(new URLSearchParams("category=plugin"), {
      kinds: new Set(["plugin", "agent-product"]),
      sources: new Set(["github"]),
      scenes: new Set(["integration", "productivity"]),
      requiresApiKey: false,
    });
    expect(params.get("category")).toBe("plugin");
    expect(parseMarketplaceFilters(params)).toEqual({
      kinds: new Set(["agent-product", "plugin"]),
      sources: new Set(["github"]),
      scenes: new Set(["integration", "productivity"]),
      requiresApiKey: false,
    });
  });

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

  it("does not expose filter values or top-level resource categories without tool supply", async () => {
    const user = userEvent.setup();
    render(
      <I18nProvider>
        <MemoryRouter initialEntries={["/marketplace"]}>
          <MarketplacePage />
        </MemoryRouter>
      </I18nProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Source" }));
    expect(screen.queryByText("Hugging Face")).toBeNull();
    expect(screen.queryByRole("tab", { name: /Content and guides/ })).toBeNull();

    await user.click(screen.getByRole("button", { name: "Tool type" }));
    const typeOptions = screen.getByRole("listbox");
    expect(within(typeOptions).getByText("Plugins")).toBeTruthy();
    expect(within(typeOptions).queryByText("External tools")).toBeNull();
    expect(within(typeOptions).queryByText("Content")).toBeNull();
  });

  it("places clear filters before the dropdowns without changing their order", async () => {
    const user = userEvent.setup();
    render(
      <I18nProvider>
        <MemoryRouter initialEntries={["/marketplace"]}>
          <MarketplacePage />
        </MemoryRouter>
      </I18nProvider>,
    );

    await user.click(screen.getByRole("button", { name: "Tool type" }));
    await user.click(screen.getByLabelText("Skill"));

    const filterBar = screen.getByLabelText("Filter");
    const buttons = within(filterBar).getAllByRole("button");
    expect(buttons[0].textContent).toBe("Clear all filters");
    expect(buttons[1].textContent).toContain("Tool type");
  });

  it("uses an installed local Agent to recommend only catalog tools", async () => {
    vi.spyOn(api, "marketplaceRecommendationAgents").mockResolvedValue([{ id: "codex", name: "Codex" }]);
    vi.spyOn(api, "recommendMarketplace").mockResolvedValue({
      agent_id: "codex",
      recommendations: [{ item_id: "skill-a", reason: "Matches multi-agent orchestration work" }],
    });
    const user = userEvent.setup();

    render(
      <I18nProvider>
        <MemoryRouter initialEntries={["/marketplace"]}>
          <MarketplacePage />
        </MemoryRouter>
      </I18nProvider>,
    );

    expect(screen.getByText(/3 tools.*Offline snapshot/)).toBeTruthy();
    const recommendButton = screen.getByRole("button", { name: "Find tools for me" });
    expect(recommendButton.classList.contains("button-primary")).toBe(true);
    await user.click(recommendButton);
    const dialog = await screen.findByRole("dialog", { name: "Tool recommendations" });

    await user.type(screen.getByLabelText("What do you want to accomplish?"), "coordinate coding agents");
    await user.click(screen.getByRole("button", { name: "Recommend tools" }));

    expect(await within(dialog).findByText("Ultracode Skill")).toBeTruthy();
    expect(within(dialog).getByText("Matches multi-agent orchestration work")).toBeTruthy();
    expect(api.recommendMarketplace).toHaveBeenCalledWith(expect.objectContaining({
      agent_id: "codex",
      need: "coordinate coding agents",
      items: expect.arrayContaining([expect.objectContaining({ id: "skill-a" })]),
    }));
  });
});
