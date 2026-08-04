import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OneAgentApiError } from "../backend/errors";
import type { AgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { AgentQuickSwitch } from "./AgentQuickSwitch";

const activateAgent = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: { activateAgent: (id: string, options: unknown) => activateAgent(id, options) },
    describeError: errors.describeError,
  };
});

beforeEach(() => {
  activateAgent.mockReset();
  activateAgent.mockResolvedValue({ ok: true, agent: "codex", restart: "", next: "", backup: null });
});

function profile(over: Partial<ProfileSummary> = {}): ProfileSummary {
  return {
    id: "team",
    label: "团队 PPIO",
    provider: "ppio",
    baseUrl: null,
    model: "deepseek/deepseek-v3",
    agentIds: ["codex"],
    activatedAt: null,
    hasKey: true,
    ...over,
  };
}

function agent(over: Partial<AgentStatus> = {}): AgentStatus {
  return { installed: true, configured: true, profileId: "team", ...over } as AgentStatus;
}

function mount(profiles: ProfileSummary[], status = agent(), onSwitched = vi.fn(), providers?: StatusResponse["providers"]) {
  render(<AgentQuickSwitch agentId="codex" status={status} profiles={profiles} providers={providers} onSwitched={onSwitched} />);
  return onSwitched;
}

describe("AgentQuickSwitch", () => {
  it("stays hidden when there is nothing to switch between", () => {
    // A single Profile means the only option is the state the Agent is already in.
    const { container } = render(
      <AgentQuickSwitch agentId="codex" status={agent()} profiles={[profile()]} onSwitched={vi.fn()} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("offers only the Profiles that already cover this Agent", () => {
    mount([
      profile(),
      profile({ id: "solo", label: "个人", agentIds: ["claude-code"] }),
      profile({ id: "alt", label: "备用" }),
    ]);
    expect(screen.getByRole("button", { name: /团队 PPIO/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /备用/ })).toBeTruthy();
    // Applying a Profile that does not list this Agent would widen its scope.
    expect(screen.queryByRole("button", { name: /个人/ })).toBeNull();
  });

  it("writes the chosen Profile without asking for a key", async () => {
    const onSwitched = mount([profile(), profile({ id: "alt", label: "备用", model: "gpt-5" })]);
    await userEvent.click(screen.getByRole("button", { name: /备用/ }));

    // apiKey is empty on purpose: the backend resolves it from the Profile secret
    // or the Provider record, so a switch never handles a plaintext key.
    await waitFor(() =>
      expect(activateAgent).toHaveBeenCalledWith(
        "codex",
        expect.objectContaining({ profileId: "alt", model: "gpt-5", apiKey: "" }),
      ),
    );
    await waitFor(() => expect(onSwitched).toHaveBeenCalledTimes(1));
  });

  it("marks the Profile in force and refuses to reapply it", () => {
    mount([profile(), profile({ id: "alt", label: "备用" })]);
    const active = screen.getByRole("button", { name: /团队 PPIO/ });
    expect(active.getAttribute("aria-pressed")).toBe("true");
    expect(active.hasAttribute("disabled")).toBe(true);
  });

  it("disables a Profile the backend would reject", () => {
    // No key and no model are the two cases ActivateAgent fails on, so failing
    // here is quieter than a round trip that errors.
    mount([profile(), profile({ id: "nokey", label: "缺 Key", hasKey: false })]);
    expect(screen.getByRole("button", { name: /缺 Key/ }).hasAttribute("disabled")).toBe(true);
  });

  it("accepts a Profile backed by the Provider key", () => {
    mount(
      [profile({ hasKey: false }), profile({ id: "provider-only", label: "Provider Key", hasKey: false })],
      agent(),
      vi.fn(),
      { ppio: { name: "PPIO", home: "", base_url: "", has_key: true } },
    );
    expect(screen.getByRole("button", { name: /Provider Key/ }).hasAttribute("disabled")).toBe(false);
  });

  it("reports a failed switch instead of looking successful", async () => {
    activateAgent.mockRejectedValue(new OneAgentApiError("写入被拒绝", "AGENT_INSTALL_FAILED", false, 500));
    const onSwitched = mount([profile(), profile({ id: "alt", label: "备用" })]);
    await userEvent.click(screen.getByRole("button", { name: /备用/ }));
    expect(await screen.findByText("写入被拒绝")).toBeTruthy();
    expect(onSwitched).not.toHaveBeenCalled();
  });
});
