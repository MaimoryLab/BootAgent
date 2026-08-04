import type { DesktopAgentStatus, ProfileSummary, StatusResponse } from "../types/api";

export const CHATGPT_DESKTOP_ID = "desktop-agent";
export const CODEX_AGENT_ID = "codex";

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
  const protocol = status.catalog?.find((item) => item.id === owner)?.protocol;
  return status.profiles.filter((profile) => {
    if (protocol && profile.protocol) return profile.protocol === protocol;
    const agentIds = profile.agentIds ?? [];
    // A binding is the only ownership signal available for legacy profiles
    // that predate agent_ids. It must not override an explicit owner list.
    return agentIds.includes(owner) || (agentIds.length === 0 && profile.id === bound);
  });
}

export function desktopProfileUsable(status: StatusResponse, profile: ProfileSummary): boolean {
  const provider = status.providers[profile.provider];
  return Boolean(provider && profile.model?.trim() && (profile.hasKey || provider.has_key));
}

export function desktopProfileIsShared(app: DesktopAgentStatus): boolean {
  return profileAgentIdForDesktop(app) !== app.id;
}
