export const platformIds = ["macos", "linux", "windows"] as const;
export type PlatformId = (typeof platformIds)[number];

export const protocolIds = ["openai", "anthropic", "responses"] as const;
export type ProtocolId = (typeof protocolIds)[number];

export type ProviderProtocolStatus =
  | "implementation-supported"
  | "release-candidate-required"
  | "verified"
  | "unsupported";

export interface SiteAgentSupport {
  managedInstall: boolean;
  officialInstallGuide: boolean;
  managedConfig: boolean;
}

export interface SiteAgent {
  id: string;
  name: string;
  group: string;
  rank: number;
  command: string | null;
  configPath: string | null;
  platforms: PlatformId[];
  lockedVersion: string | null;
  source: string | null;
  license: string | null;
  licenseUrl: string | null;
  guide: string | null;
  protocol: ProtocolId | null;
  support: SiteAgentSupport;
}

export interface SiteProviderProtocol {
  id: ProtocolId;
  status: ProviderProtocolStatus;
}

export interface SiteProvider {
  id: string;
  name: string;
  home: string;
  relationship: "none" | "referral" | "sponsor";
  disclosure: string;
  referralUrl: string;
  protocols: SiteProviderProtocol[];
  order: number;
}

export interface SiteAgentGroup {
  id: string;
  name: string;
}

export interface SiteCatalogV2 {
  schema_version: 2;
  groups: SiteAgentGroup[];
  agents: SiteAgent[];
  providers: SiteProvider[];
}

export type Compatibility = "verified" | "supported" | "preview-gate" | "unsupported";

export interface RecommendedCombination {
  agentId: string;
  providerId: string;
}

export interface ExplorerSearchState {
  agent: string | null;
  provider: string | null;
  platform: PlatformId | null;
  protocol: ProtocolId | null;
}

export function protocolStatusFor(provider: SiteProvider, protocol: ProtocolId | null): ProviderProtocolStatus {
  if (!protocol) return "unsupported";
  return provider.protocols.find((entry) => entry.id === protocol)?.status ?? "unsupported";
}

export function compatibilityFor(agent: SiteAgent, provider: SiteProvider): Compatibility {
  switch (protocolStatusFor(provider, agent.protocol)) {
    case "verified":
      return "verified";
    case "implementation-supported":
      return "supported";
    case "release-candidate-required":
      return "preview-gate";
    default:
      return "unsupported";
  }
}

export function recommendedCombination(catalog: SiteCatalogV2): RecommendedCombination | null {
  const agents = [...catalog.agents].sort((left, right) => left.rank - right.rank || left.id.localeCompare(right.id));
  const providers = [...catalog.providers].sort(
    (left, right) => left.order - right.order || left.name.localeCompare(right.name),
  );

  for (const agent of agents) {
    if (!agent.support.managedConfig || !agent.protocol) continue;
    const provider = providers.find((candidate) => {
      const compatibility = compatibilityFor(agent, candidate);
      return compatibility === "supported" || compatibility === "verified";
    });
    if (provider) return { agentId: agent.id, providerId: provider.id };
  }
  return null;
}

function isPlatform(value: string | null): value is PlatformId {
  return Boolean(value && platformIds.includes(value as PlatformId));
}

function isProtocol(value: string | null): value is ProtocolId {
  return Boolean(value && protocolIds.includes(value as ProtocolId));
}

export function parseExplorerSearch(search: string, catalog: SiteCatalogV2): ExplorerSearchState {
  const params = new URLSearchParams(search.startsWith("?") ? search.slice(1) : search);
  const selectedAgent = catalog.agents.find((agent) => agent.id === params.get("agent")) ?? null;
  let selectedProvider = catalog.providers.find((provider) => provider.id === params.get("provider")) ?? null;
  const requestedPlatform = params.get("platform");
  const requestedProtocol = params.get("protocol");

  let platform = isPlatform(requestedPlatform) ? requestedPlatform : null;
  let protocol = isProtocol(requestedProtocol) ? requestedProtocol : null;

  if (selectedAgent) {
    if (platform && !selectedAgent.platforms.includes(platform)) platform = null;
    if (protocol && selectedAgent.protocol !== protocol) protocol = null;
    if (selectedProvider && compatibilityFor(selectedAgent, selectedProvider) === "unsupported") selectedProvider = null;
  }
  if (selectedProvider && protocol && protocolStatusFor(selectedProvider, protocol) === "unsupported") {
    selectedProvider = null;
  }

  return {
    agent: selectedAgent?.id ?? null,
    provider: selectedProvider?.id ?? null,
    platform,
    protocol,
  };
}

export function serializeExplorerSearch(state: ExplorerSearchState): string {
  const params = new URLSearchParams();
  if (state.agent) params.set("agent", state.agent);
  if (state.provider) params.set("provider", state.provider);
  if (state.platform) params.set("platform", state.platform);
  if (state.protocol) params.set("protocol", state.protocol);
  return params.toString();
}
