import { render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { BootAgentApiError } from "../backend/errors";
import type { AgentCatalogItem, AgentStatus } from "../types/api";
import { AgentManageRow, compareVersions, isBehind, targetSummary, updateOffer } from "./AgentManageRow";

const launchAgent = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: { launchAgent: (agentId: string, directory: string) => launchAgent(agentId, directory) },
    describeError: errors.describeError,
    describeFailure: errors.describeFailure,
  };
});

beforeEach(() => {
  launchAgent.mockReset();
  launchAgent.mockResolvedValue({ ok: true, agent: "codex", command: "codex" });
});

const catalogAgent: AgentCatalogItem = {
  id: "codex",
  name: "Codex",
  group: "auto",
  configMode: "auto",
  selectsModel: true, guideOnly: false,
  lockedVersion: "0.145.0",
  protocol: "responses",
  platforms: ["macos", "linux", "windows"],
  platformNote: "",
  rank: 1,
  // The package contract comes from the catalog, which reads agents.lock.json.
  // It used to be a literal in the component, which is how OpenClaw ended up
  // without an update button after shipping as an npm Agent.
  packageManager: "npm",
  packageName: "@openai/codex",
};

function agentStatus(over: Partial<AgentStatus> = {}): AgentStatus {
  return {
    installed: true,
    configured: true,
    guideOnly: false,
    config: "/home/u/.codex/config.toml",
    version: "0.145.0",
    lockedVersion: "0.145.0",
    latestVersion: null,
    canInstall: true,
    provider: "ppio",
    profileId: "team",
    model: "deepseek/deepseek-v3",
    baseUrl: "https://api.ppio.com/openai",
    updatedAt: "2026-07-27T00:00:00Z",
    detected: null,
    ...over,
  };
}

function renderRow(over: Partial<AgentStatus> = {}, profileName = "团队 PPIO", catalogOver: Partial<AgentCatalogItem> = {}) {
  render(
    <MemoryRouter>
      <AgentManageRow
        agentId="codex"
        catalog={{ ...catalogAgent, ...catalogOver }}
        status={agentStatus(over)}
        providers={{
          ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" },
        }}
        profileName={profileName}
        defaultDirectory="/tmp"
      />
    </MemoryRouter>,
  );
}

describe("AgentManageRow", () => {
  it("shows what the Agent is pointed at", () => {
    renderRow();
    expect(screen.getByText("Codex")).toBeTruthy();
    expect(screen.getByRole("button", { name: "迁移对话" })).toBeTruthy();
    expect(screen.getByText("PPIO", { selector: ".agent-manage-pill" })).toBeTruthy();
    expect(within(screen.getByTestId("agent-codex").querySelector(".agent-manage-summary") as HTMLElement).getByText("deepseek/deepseek-v3")).toBeTruthy();
    expect(within(screen.getByTestId("agent-codex").querySelector(".agent-manage-summary") as HTMLElement).getByText("团队 PPIO")).toBeTruthy();
  });

  it("uses compact UI tokens instead of repeating field labels", () => {
    renderRow();
    const summary = screen.getByTestId("agent-codex").querySelector(".agent-manage-summary");
    if (!summary) throw new Error("agent summary not found");
    const summaryView = within(summary as HTMLElement);

    expect(summaryView.queryByText("Provider")).toBeNull();
    expect(summaryView.queryByText("Profile")).toBeNull();
    expect(summaryView.queryByText("模型")).toBeNull();
    expect(summaryView.queryByText("版本")).toBeNull();
    expect(summaryView.queryByText(/npm 包/)).toBeNull();
    expect(summaryView.getByText("PPIO")).toBeTruthy();
    expect(summaryView.getByText("团队 PPIO")).toBeTruthy();
    expect(summaryView.getByText("0.145.0")).toBeTruthy();
  });

  it("keeps low-frequency URL and package details behind disclosure", async () => {
    renderRow();
    const row = screen.getByTestId("agent-codex");
    const details = row.querySelector("details");
    if (!details) throw new Error("agent details disclosure not found");

    expect(details).not.toHaveAttribute("open");
    expect(within(row.querySelector(".agent-manage-summary") as HTMLElement).queryByText("https://api.ppio.com/openai")).toBeNull();
    expect(within(row.querySelector(".agent-manage-summary") as HTMLElement).queryByText("@openai/codex")).toBeNull();

    await userEvent.click(screen.getByText("详情"));

    expect(details).toHaveAttribute("open");
    expect(screen.getByText("https://api.ppio.com/openai")).toBeTruthy();
    expect(screen.getByText("@openai/codex")).toBeTruthy();
  });

  it("uses familiar icons alongside action labels", () => {
    renderRow();
    expect(screen.getByRole("link", { name: /配置/ }).querySelector("svg")).toBeTruthy();
    expect(screen.getByRole("button", { name: /更新/ }).querySelector("svg")).toBeTruthy();
    expect(screen.getByRole("button", { name: /启动/ }).querySelector("svg")).toBeTruthy();
  });

  it("names which piece is missing rather than one shared status word", () => {
    renderRow({ configured: false, provider: null, profileId: null, model: null, baseUrl: null }, "");
    expect(screen.queryByText("未记录")).toBeNull();
    // Each token names its own field. A shared "未绑定" on both, plus a separate
    // "未配置" badge, said the same thing three times without saying which of
    // the two was actually absent.
    expect(screen.getByTitle("配置模版").textContent).toBe("无配置模版");
    expect(screen.getByTitle("模型服务").textContent).toBe("无模型服务");
    expect(screen.queryByText("未配置")).toBeNull();
    expect(screen.queryByText("未绑定")).toBeNull();
  });

  it("keeps the token order fixed when a field is absent", () => {
    // The Provider token used to be dropped when empty, sliding model and
    // version left so the same field sat in a different slot per row.
    renderRow({ provider: null, model: "m-1", version: "1.0.0" }, "prod");
    const order = [...document.querySelectorAll(".agent-manage-meta .agent-manage-pill")]
      .map((pill) => pill.getAttribute("title"));
    expect(order).toEqual(["配置模版", "模型服务", "模型", "版本"]);
  });

  it("flags a version behind the locked one", () => {
    renderRow({ version: "0.144.4" });
    expect(screen.getByText("0.144.4 → 0.145.0")).toBeTruthy();
  });

  it("states a current version without editorialising", () => {
    renderRow();
    expect(screen.getByText("0.145.0")).toBeTruthy();
    expect(screen.queryByText(/最新/)).toBeNull();
  });

  it("does not call a newer local version an update", () => {
    // Caught on a real machine: Claude Code was 2.1.220 against a locked
    // 2.1.217 and the row read "可更新", inviting a downgrade.
    renderRow({ version: "2.1.220", lockedVersion: "2.1.217" });
    expect(screen.queryByText(/→/)).toBeNull();
    expect(screen.getByText(/锁定 2\.1\.217/)).toBeTruthy();
  });

  it("orders versions numerically, not as strings", () => {
    expect(compareVersions("0.9.0", "0.10.0")).toBe(-1);
    expect(compareVersions("1.2.3", "1.2.3")).toBe(0);
    expect(compareVersions("2.0.0", "1.9.9")).toBe(1);
    expect(isBehind("0.144.4", "0.145.0")).toBe(true);
    expect(isBehind("2.1.220", "2.1.217")).toBe(false);
  });

  it("edits nowhere in the row itself", () => {
    // The row links to the detail page rather than growing a form: credentials
    // and probes belong on a page with room to explain them.
    renderRow();
    expect(screen.queryByLabelText(/API Key/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /测试连接/ })).toBeNull();
  });

  it("links to the Agent's own configuration page", async () => {
    // Without this the detail page was unreachable: the route existed but nothing
    // navigated to it.
    renderRow();
    const details = screen.getByTestId("agent-codex").querySelector("details");
    if (!details) throw new Error("agent details disclosure not found");
    await userEvent.click(details.querySelector("summary") as HTMLElement);
    expect(screen.getByRole("link", { name: /配置/ }).getAttribute("href")).toBe("/agents/codex");
  });

  it("offers configuration even for an Agent that is not installed", () => {
    // Launching needs a binary; pointing it at a Provider does not, and the
    // config is what a later install will pick up.
    renderRow({ installed: false, version: null });
    expect(screen.getByRole("link", { name: /配置/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /启动/ })).toBeNull();
  });

  it("launches the Agent it belongs to", async () => {
    renderRow();
    await userEvent.click(screen.getByRole("button", { name: /启动/ }));
    await userEvent.click(screen.getByRole("dialog").querySelector("button[type=submit]") as HTMLElement);
    await waitFor(() => expect(launchAgent).toHaveBeenCalledWith("codex", expect.any(String)));
  });

  it("offers no launch for an Agent that is not installed", () => {
    renderRow({ installed: false, version: null });
    expect(screen.queryByRole("button", { name: /启动/ })).toBeNull();
  });

  it("reports a launch failure in the row instead of failing silently", async () => {
    launchAgent.mockRejectedValue(new BootAgentApiError("没有可用的终端", "PREREQUISITE_MISSING", false, 500));
    renderRow();
    await userEvent.click(screen.getByRole("button", { name: /启动/ }));
    await userEvent.click(screen.getByRole("dialog").querySelector("button[type=submit]") as HTMLElement);
    expect(await screen.findByText("没有可用的终端")).toBeTruthy();
  });
});

describe("targetSummary", () => {
  const providers = { ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai" } };

  it("reports a config BootAgent never wrote instead of calling it unconfigured", () => {
    // The defect this exists for: status reported configured=true with a null
    // provider, so a live hand-written configuration rendered as "未配置".
    const summary = targetSummary(
      agentStatus({
        provider: null,
        model: null,
        baseUrl: null,
        detected: {
          baseUrl: "https://api.other-vendor.com/v1",
          model: "gpt-5-mini",
          managedByBootAgent: false,
          unreadable: null,
        },
      }),
      providers,
    );
    expect(summary.text).toContain("https://api.other-vendor.com/v1");
    expect(summary.text).toContain("gpt-5-mini");
    expect(summary.note).toContain("非 BootAgent 写入");
  });

  it("says the file changed under us when the two sources disagree", () => {
    // Drift means someone edited the config outside BootAgent; the file wins
    // because that is what the Agent will actually load.
    const summary = targetSummary(
      agentStatus({
        detected: { baseUrl: "https://moved.example/v1", model: "m", managedByBootAgent: false, unreadable: null },
      }),
      providers,
    );
    expect(summary.text).toContain("PPIO");
    expect(summary.note).toContain("https://moved.example/v1");
  });

  it("stays quiet when the record and the file agree", () => {
    const summary = targetSummary(
      agentStatus({
        detected: {
          baseUrl: "https://api.ppio.com/openai",
          model: "deepseek/deepseek-v3",
          managedByBootAgent: true,
          unreadable: null,
        },
      }),
      providers,
    );
    expect(summary.text).toBe("PPIO · deepseek/deepseek-v3");
    expect(summary.note).toBe("");
  });

  it("shows an unreadable config as such rather than guessing", () => {
    const summary = targetSummary(
      agentStatus({
        provider: null,
        model: null,
        detected: { baseUrl: "", model: "", managedByBootAgent: false, unreadable: "TOML 无法解析：第 3 行" },
      }),
      providers,
    );
    expect(summary.text).toBe("配置无法解析");
    expect(summary.note).toContain("第 3 行");
  });

  it("still says 未配置 when there is genuinely nothing", () => {
    const summary = targetSummary(
      agentStatus({ provider: null, model: null, baseUrl: null, detected: null }),
      providers,
    );
    expect(summary.text).toBe("未配置");
  });
});

describe("updateOffer", () => {
  const npm = catalogAgent;

  it("marks an Agent behind the registry", () => {
    const offer = updateOffer(npm, agentStatus({ version: "0.145.0", latestVersion: "0.150.0" }));
    expect(offer).toEqual({ npm: true, behind: "0.150.0" });
  });

  it("says nothing when the installed version is current or newer", () => {
    // A newer local version is normal -- the user upgraded the Agent themselves
    // -- and flagging it would invite a downgrade.
    for (const [version, latest] of [["1.2.3", "1.2.3"], ["2.0.0", "1.9.9"]]) {
      expect(updateOffer(npm, agentStatus({ version, latestVersion: latest })).behind).toBe("");
    }
  });

  it("says nothing when the registry could not be reached", () => {
    // Offline or rate limited. The dot is an affordance, so an unknown answer
    // leaves the row exactly as it was rather than claiming either state.
    expect(updateOffer(npm, agentStatus({ version: "1.0.0", latestVersion: null })).behind).toBe("");
  });

  it("offers no update for an Agent that is not installed", () => {
    // `npm update -g` on a package that was never installed exits 0 and does
    // nothing, so the button would report success while the Agent stayed missing.
    const offer = updateOffer(npm, agentStatus({ installed: false, version: null, latestVersion: "9.9.9" }));
    expect(offer.npm).toBe(false);
  });

  it("offers no update for Agents npm does not manage", () => {
    // Aider comes from PyPI through uv; a guide-only entry has no package at all.
    expect(updateOffer({ ...npm, packageManager: "uv", packageName: "aider-chat" }, agentStatus()).npm).toBe(false);
    expect(updateOffer({ ...npm, packageManager: "official-script", packageName: "hermes-agent" }, agentStatus()).npm).toBe(false);
    expect(updateOffer({ ...npm, packageManager: undefined, packageName: undefined }, agentStatus()).npm).toBe(false);
    expect(updateOffer(undefined, agentStatus()).npm).toBe(false);
  });
});

describe("the update affordance in the row", () => {
  it("puts a dot on the update button when a newer version exists", async () => {
    renderRow({ version: "0.145.0", latestVersion: "0.150.0" });
    const button = screen.getByRole("button", { name: /更新/ });
    expect(button.querySelector(".agent-update-dot")).toBeTruthy();
    // The version reaches assistive technology through the title, so the dot
    // itself is decoration and must not be announced.
    expect(button.getAttribute("title")).toContain("0.150.0");
  });

  it("leaves the button bare when the Agent is current", () => {
    renderRow({ version: "0.145.0", latestVersion: "0.145.0" });
    const button = screen.getByRole("button", { name: /更新/ });
    expect(button.querySelector(".agent-update-dot")).toBeNull();
  });

  it("hides the update button entirely when the Agent is not installed", () => {
    renderRow({ installed: false, version: null, latestVersion: "9.9.9" });
    expect(screen.queryByRole("button", { name: /更新/ })).toBeNull();
  });

  it("hides the update button for official-script Agents", () => {
    renderRow({ version: "1.0.0", latestVersion: "2.0.0" }, "团队 PPIO", {
      packageManager: "official-script",
      packageName: "hermes-agent",
    });
    expect(screen.queryByRole("button", { name: /更新/ })).toBeNull();
  });

  it("reads the npm package name from the catalog, not a list of its own", async () => {
    // OpenClaw shipped as an npm Agent and was missing from that list, so it
    // lost both its update button and its package name here.
    renderRow({}, "团队 PPIO", { packageName: "openclaw" });
    await userEvent.click(screen.getByText("详情"));
    expect(screen.getByText("openclaw")).toBeTruthy();
  });
});
