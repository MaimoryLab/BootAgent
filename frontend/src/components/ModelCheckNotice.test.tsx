import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { translate } from "../i18n";
import type { ProfileModelCheck } from "../types/api";
import { describeModelCheck, ModelCheckNotice } from "./ModelCheckNotice";

function check(overrides: Partial<ProfileModelCheck> = {}): ProfileModelCheck {
  return {
    profileId: "codex-gateway",
    model: "aphrodite/venus-marble-5-5-es-cc-dst",
    status: "unavailable",
    reason: "refused",
    protocol: "responses",
    httpStatus: 400,
    detail: "model: aphrodite/venus-marble-5-5-es-cc-dst does not support endpoint: responses",
    agents: ["codex", "chatgpt-desktop"],
    ...overrides,
  };
}

function renderNotice(value: ProfileModelCheck, onUseConversion?: () => void) {
  return render(
    <MemoryRouter>
      <ModelCheckNotice check={value} providerId="gateway" nameOf={() => "Codex"} returnTo="/profiles" onUseConversion={onUseConversion} onDismiss={vi.fn()} />
    </MemoryRouter>,
  );
}

const hint = "这个模型服务可能不支持该 Agent 的请求格式，可以换一个模型，或用兼容模式在本机转换";

describe("model check compatibility mode remedy", () => {
  it.each<Partial<ProfileModelCheck>>([
    { protocol: "responses" },
    { protocol: "anthropic", detail: "" },
  ])("offers compatibility mode when $protocol is refused", (overrides) => {
    const useConversion = vi.fn();
    renderNotice(check(overrides), useConversion);

    expect(screen.getByRole("alert").textContent).toContain("HTTP 400");
    expect(screen.getByText(hint)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "使用兼容模式" }));
    expect(useConversion).toHaveBeenCalledOnce();
  });

  it("keeps the hint but offers no button where no Agent can use compatibility mode", () => {
    renderNotice(check());

    expect(screen.getByText(hint)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "使用兼容模式" })).toBeNull();
  });

  it.each<Partial<ProfileModelCheck>>([
    { reason: "key", httpStatus: 401 },
    { reason: "no_key", status: "unverified" },
    { reason: "network", status: "unverified" },
    { reason: "transient", status: "unverified", httpStatus: 500 },
    { protocol: "openai" },
    { status: "available", reason: undefined },
  ])("does not suggest compatibility mode for $reason over $protocol", (overrides) => {
    renderNotice(check(overrides), vi.fn());

    expect(screen.queryByRole("button", { name: "使用兼容模式" })).toBeNull();
    expect(screen.queryByText(hint)).toBeNull();
  });

  it("provides the remedy in English", () => {
    const parts = describeModelCheck(check(), (key, values) => translate("en", key, values), () => "Codex", ", ");

    expect(parts.hint).toBe("This model service may not accept this Agent's request format; try another model, or use compatibility mode to convert on this computer");
  });
});
