import agentLock from "../../../agents.lock.json";
import providerConfig from "../../../providers.lock.json";
/* The site-facing shapes are declared once, in explorer.ts, because the explorer
   is what constrains them: it narrows platforms and protocols to the ids it can
   actually render a compatibility verdict for. Re-declaring them here would let
   a new protocol reach the pages while the explorer silently calls it
   unsupported. */
import type { PlatformId, ProtocolId, SiteAgent, SiteCatalogV2, SiteProvider, ProviderProtocolStatus } from "./explorer";

const adapterProtocols: Record<string, ProtocolId> = {
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
  command?: string;
  config_mode: "auto" | "guide";
  config_adapter?: string;
  config_path?: string;
  guide?: string;
  package?: {
    version?: string;
    source?: string;
    license?: string;
    license_url?: string;
  };
}

/* The group ids come from agents.lock.json, but the labels do not live there —
   the lock file is the installer's contract and has no room for display copy.
   Anything not listed falls back to its own id, so a new group appears on the
   site as soon as it appears in the lock rather than vanishing from the list. */
const groupNames: Record<string, string> = {
  auto: "One-click configurable",
  gateway: "Gateway agents",
  platform: "Official account agents",
  ide: "IDE extensions",
};

interface ProviderSource {
  name?: string;
  home: string;
  relationship?: "none" | "referral" | "sponsor";
  disclosure?: string;
  referral_url?: string;
  order?: number;
  protocols?: Record<string, string>;
}


const agents = Object.entries(agentLock.agents as Record<string, AgentSource>)
  .map(([id, meta]): SiteAgent => {
    const managedConfig = meta.config_mode === "auto" && Boolean(meta.config_adapter);
    return {
      id,
      name: meta.name ?? id,
      group: meta.group ?? "other",
      rank: meta.rank ?? 99,
      /* Both are shown on the site so a reader can check what OneAgent will run
         and which file it will write before installing anything. guide-only
         agents have neither, and null keeps that visible rather than printing an
         empty string that looks like a missing value. */
      command: meta.command ?? null,
      configPath: meta.config_path ?? null,
      platforms: (meta.platforms ?? []) as PlatformId[],
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
    protocols: Object.entries(meta.protocols ?? {}).map(([protocolId, status]) => ({
      id: protocolId as ProtocolId,
      status: status as ProviderProtocolStatus,
    })),
    order: meta.order ?? 99,
  }))
  .sort((left, right) => left.order - right.order || left.name.localeCompare(right.name));

const groups = [...new Set(agents.map((agent) => agent.group))].map((id) => ({
  id,
  name: groupNames[id] ?? id,
}));

/* Typed as the schema the pages and the explorer already consume, so this module
   is a drop-in for the generated catalog.json it replaces. schema_version is
   asserted rather than read from the lock file: it versions this site-facing
   shape, not the installer contract the lock file carries. */
export const catalog: SiteCatalogV2 = { schema_version: 2, groups, agents, providers };
