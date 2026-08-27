import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { api } from "../backend/api";
import { I18nProvider } from "../i18n";
import type { MarketplaceRecommendationHistory } from "../types/api";
import { MarketplaceRecommendationDetailPage } from "./MarketplaceRecommendationDetailPage";

const history: MarketplaceRecommendationHistory = {
  id: "rec-1", created_at: "2026-08-27T14:49:27Z", agent_id: "codex", need: "远程管理 Agent", catalog_version: "2.3.0",
  results: [
    { item_id: "github-remote-one", name: "Remote One", reason: "支持远程管理", category: "ai-product", source: "GitHub" },
    { item_id: "github-remote-two", name: "Remote Two", reason: "支持会话控制", category: "plugin", source: "GitHub" },
    { item_id: "github-remote-three", name: "Remote Three", reason: "支持移动端访问", category: "ai-product", source: "GitHub" },
    { item_id: "github-remote-four", name: "Remote Four", reason: "提供安全控制", category: "ai-product", source: "GitHub" },
  ],
};

describe("MarketplaceRecommendationDetailPage", () => {
  beforeEach(() => { vi.spyOn(api, "listRecommendationHistory").mockResolvedValue([history]); });

  it("shows every recommendation and its reason, including the fourth result", async () => {
    render(<I18nProvider><MemoryRouter initialEntries={["/marketplace/recommendations/rec-1"]}><Routes><Route path="/marketplace/recommendations/:historyId" element={<MarketplaceRecommendationDetailPage />} /></Routes></MemoryRouter></I18nProvider>);
    expect(await screen.findByText("Remote Four")).toBeTruthy();
    expect(screen.getByText("支持移动端访问")).toBeTruthy();
    expect(screen.queryAllByRole("link", { name: /查看工具/ })).toHaveLength(0);
  });
});
