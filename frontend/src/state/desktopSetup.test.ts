import { describe, expect, it } from "vitest";

import type { DesktopAgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { desktopProfileIsShared, desktopProfileUsable, desktopProfiles, profileAgentIdForDesktop } from "./desktopSetup";

function app(overrides: Partial<DesktopAgentStatus> = {}): DesktopAgentStatus {
  return {
    id: "desktop-agent",
    name: "ChatGPT Desktop",
    installed: true,
    supported: true,
    version: null,
    source: "macos-dmg",
    ...overrides,
  };
}

function profile(id: string, agentIds: string[]): ProfileSummary {
  return { id, label: id, provider: "ppio", baseUrl: null, model: "model-a", agentIds, activatedAt: null, hasKey: true };
}

function status(profiles: ProfileSummary[], agents: StatusResponse["agents"] = {}): StatusResponse {
  return { profiles, agents, providers: {} } as StatusResponse;
}

describe("desktop profile mapping", () => {
  it("shares ChatGPT profiles with Codex but scopes other apps to themselves", () => {
    const chatGPT = app({ profileAgentId: "stale-owner" });
    const workbuddy = app({ id: "workbuddy", name: "WorkBuddy", profileAgentId: "workbuddy" });
    expect(profileAgentIdForDesktop(chatGPT)).toBe("codex");
    expect(desktopProfileIsShared(chatGPT)).toBe(true);
    expect(profileAgentIdForDesktop(workbuddy)).toBe("workbuddy");
    expect(desktopProfileIsShared(workbuddy)).toBe(false);
  });

  it("includes the active binding only for legacy profiles without AgentIDs", () => {
    const chatGPT = app({ profileId: "legacy" });
    const chatGPTProfiles = [profile("legacy", []), profile("other", ["codex"]), profile("wrong-owner", ["workbuddy"])] as ProfileSummary[];
    expect(desktopProfiles(status(chatGPTProfiles, { codex: { profileId: "legacy" } as StatusResponse["agents"][string] }), chatGPT).map(({ id }) => id)).toEqual(["legacy", "other"]);

    const workbuddyProfiles = [profile("legacy", []), profile("wrong-owner", ["codex"]), profile("workbuddy", ["workbuddy"])] as ProfileSummary[];
    expect(desktopProfiles(status(workbuddyProfiles, { workbuddy: { profileId: "wrong-owner" } as StatusResponse["agents"][string] }), app({ id: "workbuddy" })).map(({ id }) => id)).toEqual(["workbuddy"]);
  });

  it("rejects a profile whose Provider no longer exists", () => {
    const candidate = profile("team", ["codex"]);
    expect(desktopProfileUsable(status([candidate]), candidate)).toBe(false);
  });
});
