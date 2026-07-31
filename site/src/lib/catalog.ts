import agentLock from "../../../agents.lock.json";
import providerConfig from "../../../providers.lock.json";
import type { AgentSupport } from "./downloads";

const adapterProtocols: Record<string, string> = {
  codex: "responses",
  "claude-code": "anthropic",
  opencode: "openai",
  "kilo-cli": "openai",
  aider: "openai",
};

interface AgentSource {
  name?: string;
  group?: string;
  rank?: number;
  platforms?: string[];
  config_mode: "auto" | "guide";
  config_adapter?: string;
  guide?: string;
  package?: {
    version?: string;
    source?: string;
    license?: string;
    license_url?: string;
  };
}

interface ProviderSource {
  name?: string;
  home: string;
  relationship?: "none" | "referral" | "sponsor";
  disclosure?: string;
  referral_url?: string;
  order?: number;
  protocols?: Record<string, string>;
}

export interface SiteAgent {
  id: string;
  name: string;
  group: string;
  rank: number;
  platforms: string[];
  lockedVersion: string | null;
  source: string | null;
  license: string | null;
  licenseUrl: string | null;
  guide: string | null;
  protocol: string | null;
  support: AgentSupport;
}

export interface SiteProvider {
  id: string;
  name: string;
  home: string;
  relationship: "none" | "referral" | "sponsor";
  disclosure: string;
  referralUrl: string;
  protocols: Array<{ id: string; status: string }>;
  order: number;
}

const agents = Object.entries(agentLock.agents as Record<string, AgentSource>)
  .map(([id, meta]): SiteAgent => {
    const managedConfig = meta.config_mode === "auto" && Boolean(meta.config_adapter);
    return {
      id,
      name: meta.name ?? id,
      group: meta.group ?? "other",
      rank: meta.rank ?? 99,
      platforms: meta.platforms ?? [],
      lockedVersion: meta.package?.version ?? null,
      source: meta.package?.source ?? null,
      license: meta.package?.license ?? null,
      licenseUrl: meta.package?.license_url ?? null,
      guide: meta.guide ?? null,
      protocol: managedConfig ? adapterProtocols[meta.config_adapter!] ?? null : null,
      support: {
        managedInstall: meta.config_mode === "auto" && Boolean(meta.package),
        officialInstallGuide: meta.config_mode === "guide",
        managedConfig,
      },
    };
  })
  .sort((left, right) => left.rank - right.rank || left.id.localeCompare(right.id));

const providers = Object.entries(providerConfig.providers as Record<string, ProviderSource>)
  .map(([id, meta]): SiteProvider => ({
    id,
    name: meta.name ?? id,
    home: meta.home,
    relationship: meta.relationship ?? "none",
    disclosure: meta.disclosure ?? "",
    referralUrl: meta.referral_url ?? "",
    protocols: Object.entries(meta.protocols ?? {}).map(([protocolId, status]) => ({ id: protocolId, status })),
    order: meta.order ?? 99,
  }))
  .sort((left, right) => left.order - right.order || left.name.localeCompare(right.name));

export const catalog = { agents, providers };
