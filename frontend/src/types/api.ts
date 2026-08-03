import type * as AppModels from "../../bindings/github.com/MaimoryLab/OneAgent/internal/app/models.js";
import type * as BindingModels from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/models.js";
import type * as CatalogModels from "../../bindings/github.com/MaimoryLab/OneAgent/internal/catalog/models.js";
import type * as PlatformModels from "../../bindings/github.com/MaimoryLab/OneAgent/internal/platform/models.js";
import type * as ProviderModels from "../../bindings/github.com/MaimoryLab/OneAgent/internal/provider/models.js";

type PlatformId = "macos" | "windows" | "linux";
type AgentGroupId = "auto" | "gateway" | "platform" | "ide";
export type ProviderId = string;
export type ProtocolId = "openai" | "anthropic" | "responses";

export const PROTOCOL_LABELS: Record<ProtocolId, string> = {
  openai: "OpenAI Chat Completions",
  anthropic: "Anthropic Messages",
  responses: "OpenAI Responses",
};

// The generated models are the backend DTO source of truth. These aliases only
// narrow catalog-controlled strings and non-null successful responses for UI use.
export type AgentCatalogItem = Omit<CatalogModels.CatalogItem, "group" | "configMode" | "protocol" | "platforms"> & {
  group: AgentGroupId;
  configMode: "auto" | "guide";
  protocol: ProtocolId | null;
  platforms: PlatformId[];
};

export type AgentStatus = AppModels.AgentStatus;
export type RuntimeStatus = AppModels.RuntimeStatus;
export type InstallRuntimeResult = Omit<AppModels.InstallRuntimeResult, "runtimes"> & { runtimes: RuntimeStatus[] };
export type Settings = AppModels.Settings;
export type ActivateAgentResponse = BindingModels.ActivateResponse;
export type LaunchAgentResponse = BindingModels.LaunchResponse;
export type OpenRegistrationResponse = BindingModels.OpenRegistrationResponse;
export type ProviderEntry = ProviderModels.Entry;
export type SaveProviderInput = BindingModels.SaveProviderRequest;
export type SaveProviderResult = AppModels.SaveProviderResult;
export type ProfileSummary = Omit<AppModels.ProfileSummary, "agentIds"> & { agentIds: string[] };

export type StatusResponse = Omit<
  AppModels.StatusResponse,
  "platform" | "capabilities" | "agents" | "catalog" | "groups" | "providers" | "mirrors" | "paths" | "backups" | "profiles" | "runtimes"
> & {
  platform: Omit<PlatformModels.Info, "os"> & { os: PlatformId };
  capabilities: Omit<AppModels.Capabilities, "canInstall" | "missingRuntime" | "supportedAgentIds"> & {
    canInstall: Record<string, boolean>;
    /** Agent id -> runtime id that must be installed before the Agent can be. */
    missingRuntime: Record<string, string>;
    supportedAgentIds: string[];
  };
  runtimes: RuntimeStatus[];
  agents: Record<string, AgentStatus>;
  catalog: AgentCatalogItem[];
  groups: Array<Omit<CatalogModels.Group, "id"> & { id: AgentGroupId }>;
  providers: Record<string, CatalogModels.Provider>;
  mirrors: CatalogModels.Mirror[];
  paths: Record<string, string>;
  backups: Record<string, boolean>;
  profiles: ProfileSummary[];
};

export type ProbeResponse = Omit<BindingModels.ProbeResponse, "protocol" | "protocols"> & {
  protocol?: ProtocolId;
  protocols?: Partial<Record<ProtocolId, ProbeResponse>>;
};

export type ModelsResponse = Omit<BindingModels.ModelsResponse, "models" | "protocol" | "protocols"> & ProbeResponse & {
  models: string[];
};

type AgentResultStatus = "configured" | "guide-only" | "installed" | "skipped" | "failed";

export type AgentInstallResult = Omit<
  BindingModels.AgentInstallResult,
  "status" | "config" | "installed" | "version" | "lockedVersion" | "registry" | "code" | "error_code" | "message"
> & {
  status: AgentResultStatus;
  config?: string;
  installed?: boolean;
  version?: string | null;
  lockedVersion?: string | null;
  registry?: string;
  code?: number;
  error_code?: string;
  message?: string;
};

export type InstallResponse = Omit<BindingModels.InstallResponse, "results" | "probe" | "probes"> & {
  results: AgentInstallResult[];
  probe: ProbeResponse | null;
  probes?: Partial<Record<ProtocolId, ProbeResponse>>;
};

export type InstallOutput =
  | { kind: "command"; args: string[] }
  | { kind: "output"; stream: "stdout" | "stderr"; text: string }
  /** total is 0 when the server sent no Content-Length. */
  | { kind: "progress"; target: string; received: number; total: number };

export type InstallRequest = Pick<
  BindingModels.InstallRequest,
  "api_key" | "model" | "configure" | "install_agent" | "skip_test"
> & {
  agents: string[];
  provider: ProviderId;
  api_base_url?: string;
  small_fast_model?: string;
  agent_version?: string;
  profile_agents?: string[];
  registry?: string;
  profile_id?: string;
  profile_label?: string;
  timeout?: number;
};
