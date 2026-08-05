import type { DesktopAgentStatus, ProfileSummary, StatusResponse } from "../types/api";

export const CHATGPT_DESKTOP_ID = "desktop-agent";
export const CODEX_AGENT_ID = "codex";
export const WORKBUDDY_DESKTOP_ID = "workbuddy";

export function desktopApps(status: StatusResponse): DesktopAgentStatus[] {
  const values = status.desktopAgents?.length ? status.desktopAgents : [status.desktopAgent];
  const seen = new Set<string>();
  return values.filter((app) => {
    if (!app?.id || seen.has(app.id)) return false;
    seen.add(app.id);
    return true;
  });
}

export function selectedDesktopApp(status: StatusResponse, selectedAgentIds: string[]): DesktopAgentStatus | undefined {
  const selected = selectedAgentIds[0];
  return desktopApps(status).find((app) => app.id === selected);
}

export function desktopProtocol(status: StatusResponse, app: DesktopAgentStatus): string {
  if (app.protocol?.trim()) return app.protocol;
  const owner = profileAgentIdForDesktop(app);
  return status.catalog?.find((item) => item.id === owner)?.protocol || (app.id === WORKBUDDY_DESKTOP_ID ? "openai" : "");
}

/**
 * Desktop apps own a profile unless their config contract explicitly points at
 * another Agent. ChatGPT Desktop is the one current shared-config exception.
 */
export function profileAgentIdForDesktop(app: DesktopAgentStatus): string {
  // ChatGPT Desktop is a contract-level exception. Do not let a stale or
  // malformed projection make it look like it owns a separate profile.
  if (app.id === CHATGPT_DESKTOP_ID) return CODEX_AGENT_ID;
  return app.profileAgentId?.trim() || app.id;
}

export function desktopProfiles(status: StatusResponse, app: DesktopAgentStatus): ProfileSummary[] {
  const owner = profileAgentIdForDesktop(app);
  const bound = status.agents[owner]?.profileId;
  const protocol = desktopProtocol(status, app);
  return status.profiles.filter((profile) => {
    if (protocol && profile.protocol) return profile.protocol === protocol;
    return profile.id === bound;
  });
}

export function desktopProfileUsable(status: StatusResponse, profile: ProfileSummary): boolean {
  const provider = status.providers[profile.provider];
  return Boolean(provider && profile.model?.trim() && (profile.hasKey || provider.has_key));
}

export function desktopProfileIsShared(app: DesktopAgentStatus): boolean {
  return profileAgentIdForDesktop(app) !== app.id;
}
