import { describe, expect, it } from "vitest";

import type { DesktopAgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { desktopApps, desktopProfileIsShared, desktopProfileUsable, desktopProfiles, profileAgentIdForDesktop } from "./desktopSetup";

function app(overrides: Partial<DesktopAgentStatus> = {}): DesktopAgentStatus {
  return {
    id: "chatgpt-desktop",
    name: "ChatGPT Desktop",
    installed: true,
    supported: true,
    version: null,
    source: "macos-dmg",
    protocol: "responses",
    profileAgentId: "codex",
    profileId: null,
    ...overrides,
  };
}

function profile(id: string, protocol: ProfileSummary["protocol"]): ProfileSummary {
  return { id, label: id, provider: "ppio", baseUrl: null, model: "model-a", protocol, activatedAt: null, hasKey: true };
}

function status(profiles: ProfileSummary[], agents: StatusResponse["agents"] = {}): StatusResponse {
  return {
    profiles,
    agents,
    providers: {},
    desktopAgents: [],
    catalog: [
      { id: "codex", protocol: "responses" },
      { id: "workbuddy", protocol: "openai" },
    ],
  } as unknown as StatusResponse;
}

describe("desktop profile mapping", () => {
  it("uses only the backend desktop Agent list", () => {
    const base = status([]);
    expect(desktopApps(base)).toEqual([]);
    expect(desktopApps({ ...base, desktopAgents: [app(), app({ id: "workbuddy", name: "WorkBuddy", profileAgentId: "workbuddy", protocol: "openai" })] })).toHaveLength(2);
  });

  it("uses the backend profile owner for each desktop Agent", () => {
    const chatGPT = app();
    const workbuddy = app({ id: "workbuddy", name: "WorkBuddy", profileAgentId: "workbuddy" });
    expect(profileAgentIdForDesktop(chatGPT)).toBe("codex");
    expect(desktopProfileIsShared(chatGPT)).toBe(true);
    expect(profileAgentIdForDesktop(workbuddy)).toBe("workbuddy");
    expect(desktopProfileIsShared(workbuddy)).toBe(false);
    const future = app({ id: "future-desktop", profileAgentId: "future-owner" });
    expect(profileAgentIdForDesktop(future)).toBe("future-owner");
    expect(desktopProfileIsShared(future)).toBe(true);
  });

  it("uses the protocol plus the active binding for legacy profiles", () => {
    const chatGPT = app({ profileId: "legacy" });
    const chatGPTProfiles = [profile("legacy", ""), profile("other", "responses"), profile("wrong-owner", "openai")];
    expect(desktopProfiles(status(chatGPTProfiles, { codex: { profileId: "legacy" } as StatusResponse["agents"][string] }), chatGPT).map(({ id }) => id)).toEqual(["legacy", "other"]);

    const workbuddyProfiles = [profile("legacy", ""), profile("wrong-owner", "responses"), profile("workbuddy", "openai")];
    expect(desktopProfiles(status(workbuddyProfiles, { workbuddy: { profileId: "wrong-owner" } as StatusResponse["agents"][string] }), app({ id: "workbuddy", protocol: "openai", profileAgentId: "workbuddy" })).map(({ id }) => id)).toEqual(["workbuddy"]);
  });

  it("rejects a profile whose Provider no longer exists", () => {
    const candidate = profile("team", "responses");
    expect(desktopProfileUsable(status([candidate]), candidate)).toBe(false);
  });
});
