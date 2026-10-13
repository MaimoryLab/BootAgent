import type { Translate } from "../i18n";
import type { ProbeResponse, ProfileSummary, ProtocolId, StatusResponse } from "../types/api";

export function isConverterID(id: string): boolean {
  return id.startsWith("bootagent-converter-") || id.startsWith("bootagent_converter_");
}

export function converterProfileName(id: string, fallback: string, t: Translate): string {
  const kind = id.startsWith("bootagent-converter-")
    ? id.slice("bootagent-converter-".length)
    : id.startsWith("bootagent_converter_")
      ? id.slice("bootagent_converter_".length)
      : "";
  // An Agent's own route. Normally shown by its target instead (see
  // conversionDisplay); this is the fallback when the target is gone.
  if (kind.startsWith("for-")) return t("兼容模式");
  switch (kind) {
    case "anthropic": return t("兼容模式（Claude Code）");
    case "responses": return t("兼容模式（Codex）");
    case "chat": return t("兼容模式（OpenCode / Aider）");
    default: return fallback;
  }
}

/**
 * The Profile an Agent's requests really go to when it uses compatibility
 * mode. Each Agent has a route of its own, so this differs per Agent.
 */
export function conversionTargetFor(status: StatusResponse | null, agentId: string): ProfileSummary | undefined {
  const targetId = status?.conversionRoutes?.[agentId];
  return targetId ? status?.profiles.find((profile) => profile.id === targetId) : undefined;
}

/**
 * What an Agent row shows for the Profile it is bound to. A converter Profile
 * is named after the model service behind it, so the row reads "PPIO ·
 * deepseek-v3 (compatibility mode)" rather than the adapter's alias.
 */
export function conversionDisplay(status: StatusResponse, agentId: string, bound: ProfileSummary | undefined, t: Translate): { profile?: ProfileSummary; name: string } {
  if (!bound) return { profile: undefined, name: "" };
  if (!isConverterID(bound.id)) return { profile: bound, name: bound.label || bound.id };
  const target = conversionTargetFor(status, agentId);
  if (!target) return { profile: bound, name: converterProfileName(bound.id, bound.label || bound.id, t) };
  return { profile: target, name: t("{name}（兼容模式）", { name: target.label || target.id }) };
}

/**
 * Whether compatibility mode can serve an Agent speaking `protocol`. The local
 * adapter converts Anthropic Messages and OpenAI Responses into Chat
 * Completions; an Agent already speaking Chat Completions has nothing to gain.
 */
export function canConvertProtocol(protocol: string | null | undefined): protocol is ProtocolId {
  return protocol === "anthropic" || protocol === "responses";
}

/** A connection test's verdict that compatibility mode would make `protocol` work. */
export function probeOffersConversion(result: ProbeResponse | null, protocol: string | null | undefined): boolean {
  return Boolean(result && !result.ok && canConvertProtocol(protocol) && result.convertible?.includes(protocol));
}

/**
 * A Provider's name as the user should read it. The adapter's own Providers
 * carry internal names ("BootAgent Converter anthropic"); an Agent pointed at
 * one is using compatibility mode, which is what it says instead.
 */
export function providerDisplayName(providers: StatusResponse["providers"], providerId: string, t: Translate): string {
  if (isConverterID(providerId)) return t("本机转换");
  return providers[providerId]?.name || providerId;
}

/** The Providers a picker should offer: every one but the adapter's own. */
export function userProviders(providers: StatusResponse["providers"]): StatusResponse["providers"] {
  return Object.fromEntries(Object.entries(providers).filter(([id]) => !isConverterID(id)));
}

export interface ConversionUser {
  id: string;
  name: string;
  /** The Profile its requests go to; undefined when that Profile was deleted. */
  target?: ProfileSummary;
}

/** Agents currently pointed at the adapter, each with its own target. */
export function agentsUsingConversion(status: StatusResponse | null): ConversionUser[] {
  if (!status) return [];
  const users: ConversionUser[] = [];
  for (const agent of status.catalog) {
    const profileId = status.agents[agent.id]?.profileId;
    if (profileId && isConverterID(profileId)) users.push({ id: agent.id, name: agent.name, target: conversionTargetFor(status, agent.id) });
  }
  for (const app of status.desktopAgents ?? []) {
    if (app.profileId && isConverterID(app.profileId)) users.push({ id: app.id, name: app.name, target: conversionTargetFor(status, app.id) });
  }
  return users;
}
