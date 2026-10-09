import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
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

function renderNotice(value: ProfileModelCheck) {
  return render(
    <MemoryRouter>
      <Routes>
        <Route path="/" element={<ModelCheckNotice check={value} providerId="gateway" nameOf={() => "Codex"} returnTo="/profiles" onDismiss={vi.fn()} />} />
        <Route path="/conversion" element={<div>API 协议适配设置</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("Codex model check guidance", () => {
  it("offers an explicit adapter path when the Responses request is refused", () => {
    renderNotice(check());

    expect(screen.getByRole("alert").textContent).toContain("HTTP 400");
    expect(screen.getByRole("alert").textContent).toContain("does not support endpoint: responses");
    expect(screen.getByText("Codex 使用 Responses API。请选择支持 Responses 的模型；如果模型服务仅支持 Chat Completions，可使用 API 协议适配，并应用适配器生成的 Codex 配置模版。")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "打开 API 协议适配" }));
    expect(screen.getByText("API 协议适配设置")).toBeTruthy();
  });

  it.each<Partial<ProfileModelCheck>>([
    { reason: "key", httpStatus: 401 },
    { reason: "no_key", status: "unverified" },
    { reason: "network", status: "unverified" },
    { reason: "transient", status: "unverified", httpStatus: 500 },
    { protocol: "anthropic" },
    { protocol: "openai" },
    { status: "available", reason: undefined },
  ])("does not suggest protocol adaptation for $reason over $protocol", (overrides) => {
    renderNotice(check(overrides));

    expect(screen.queryByRole("button", { name: "打开 API 协议适配" })).toBeNull();
    expect(screen.queryByText(/Codex 使用 Responses API/)).toBeNull();
  });

  it("provides the Responses remedy in English", () => {
    const parts = describeModelCheck(check(), (key, values) => translate("en", key, values), () => "Codex", ", ");

    expect(parts.hint).toBe("Codex uses the Responses API. Choose a model that supports Responses. If the provider only supports Chat Completions, use the API protocol adapter and apply its generated Codex Profile.");
  });
});
