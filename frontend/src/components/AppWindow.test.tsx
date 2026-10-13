import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let mockStatus: { migrationNotice?: string; catalog: unknown[] } = { catalog: [] };

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));
vi.mock("./NavigationSidebar", () => ({ NavigationSidebar: () => null }));
vi.mock("../state/WizardContext", () => ({
  useWizard: () => ({ state: { status: mockStatus } }),
}));

import { I18nProvider } from "../i18n";
import { FeedbackProvider } from "../state/FeedbackContext";
import { AppWindow } from "./AppWindow";
import { PageScaffold } from "./PageScaffold";

const renderWindow = () =>
  render(
    <I18nProvider>
      <FeedbackProvider>
        <MemoryRouter>
          <AppWindow>
            <PageScaffold title="Page" />
          </AppWindow>
        </MemoryRouter>
      </FeedbackProvider>
    </I18nProvider>,
  );

beforeEach(() => localStorage.setItem("bootagent.locale", "zh-CN"));
afterEach(() => {
  localStorage.clear();
  mockStatus = { catalog: [] };
});

describe("AppWindow migration notice", () => {
  it("reports a failed migration as a receipt instead of a paragraph above the page", async () => {
    mockStatus = { catalog: [], migrationNotice: "无法迁移旧 BootAgent 配置：inspect legacy configuration: denied" };
    const { container } = renderWindow();

    const receipt = await screen.findByRole("alert");
    expect(receipt).toHaveTextContent("没能迁移旧版配置");
    expect(container.querySelector(".app-main")?.firstElementChild).toHaveClass("page-scaffold");
  });

  it("reports a completed migration as a warning that waits to be read", async () => {
    mockStatus = { catalog: [], migrationNotice: "OneAgent configuration was migrated to BootAgent." };
    renderWindow();

    expect(await screen.findByRole("alert")).toHaveTextContent("已迁移旧版配置");
  });

  it("shows nothing without a notice", () => {
    renderWindow();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
