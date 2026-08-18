import type { DesktopAgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { isConverterID } from "./conversion";

export function desktopApps(status: StatusResponse): DesktopAgentStatus[] {
  const seen = new Set<string>();
  return (status.desktopAgents ?? []).filter((app) => {
    if (!app?.id || seen.has(app.id)) return false;
    seen.add(app.id);
    return true;
  });
}

export function selectedDesktopApp(status: StatusResponse, selectedAgentIds: string[]): DesktopAgentStatus | undefined {
  const selected = selectedAgentIds[0];
  return desktopApps(status).find((app) => app.id === selected);
}

export function desktopProtocol(app: DesktopAgentStatus): string {
  return app.protocol?.trim() || "";
}

export function profileAgentIdForDesktop(app: DesktopAgentStatus): string {
  return app.profileAgentId.trim();
}

export function desktopProfiles(status: StatusResponse, app: DesktopAgentStatus): ProfileSummary[] {
  const owner = profileAgentIdForDesktop(app);
  const bound = status.agents[owner]?.profileId;
  const protocol = desktopProtocol(app);
  return status.profiles.filter((profile) => {
    if (protocol && profile.protocol) return profile.protocol === protocol;
    return profile.id === bound;
  });
}

export function desktopProfileUsable(status: StatusResponse, profile: ProfileSummary, app?: DesktopAgentStatus | null): boolean {
  const provider = status.providers[profile.provider];
  const model = profile.model?.trim() || "";
  if (app?.id === "claude-desktop" && !model.toLowerCase().includes("claude")) return false;
  return Boolean(provider && model && (provider.has_key || isConverterID(profile.id)));
}

export function desktopProfileIsShared(app: DesktopAgentStatus): boolean {
  return profileAgentIdForDesktop(app) !== app.id;
}
