import type * as AppModels from "../../bindings/github.com/MaimoryLab/BootAgent/internal/app/models.js";
import type * as BindingModels from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/models.js";
import type * as CatalogModels from "../../bindings/github.com/MaimoryLab/BootAgent/internal/catalog/models.js";
import type * as MCPModels from "../../bindings/github.com/MaimoryLab/BootAgent/internal/mcp/models.js";
import type * as PlatformModels from "../../bindings/github.com/MaimoryLab/BootAgent/internal/platform/models.js";
import type * as ProviderModels from "../../bindings/github.com/MaimoryLab/BootAgent/internal/provider/models.js";

type PlatformId = "macos" | "windows" | "linux";
type AgentGroupId = "auto" | "gateway" | "platform" | "ide";
export type ProviderId = string;
export type ProtocolId = "openai" | "anthropic" | "responses";
export type MCPSpec = MCPModels.Spec;
export type MCPVariant = MCPModels.Variant;
export type MCPServerSummary = AppModels.MCPServerSummary;
export type MCPServerDetail = AppModels.MCPServerDetail;
export type MCPScanResult = AppModels.MCPScanResult;
export type MCPApplyRequest = AppModels.MCPApplyRequest;
export type MCPApplyResult = AppModels.MCPApplyResult;
export type SkillSummary = AppModels.SkillSummary;
export type SkillCandidate = AppModels.SkillCandidate;
export type SkillScanResult = AppModels.SkillScanResult & { preview_token?: string };
export type SkillImportPreview = AppModels.SkillImportPreview;
export type SkillApplyRequest = AppModels.SkillApplyRequest;
export type SkillApplyResult = AppModels.SkillApplyResult;
export type SkillChange = AppModels.SkillChange;
export type SkillBackupSummary = AppModels.SkillBackupSummary;
export type SkillUninstallResult = AppModels.SkillUninstallResult;

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
export type DesktopAgentStatus = Omit<AppModels.DesktopAgentStatus, "protocol"> & { protocol: ProtocolId | "" };
export type DesktopAgentActionResult = Omit<AppModels.DesktopAgentActionResult, "app"> & { app: DesktopAgentStatus };
export type DesktopAgentProfileResult = AppModels.DesktopAgentProfileResult;
export type InstallRuntimeResult = Omit<AppModels.InstallRuntimeResult, "runtimes"> & { runtimes: RuntimeStatus[] };
export type Settings = AppModels.Settings;
export type SettingsPatch = AppModels.SettingsPatch;
export type TerminalOption = AppModels.TerminalOption;
export type ConversionConfig = AppModels.ConversionConfig;
export type ActivateAgentResponse = BindingModels.ActivateResponse;
export type LaunchAgentResponse = BindingModels.LaunchResponse;
export type AgentUninstallResult = AppModels.AgentUninstallResult;
export type AgentUpdateResult = AppModels.AgentUpdateResult;
export type ConversationMigrationResult = AppModels.ConversationMigrationResult;
export type TaskHistoryRecord = AppModels.TaskHistoryRecord;
export type MarketplaceRecommendationAgent = AppModels.MarketplaceRecommendationAgent;
export type MarketplaceKnowledgeItem = AppModels.MarketplaceKnowledgeItem;
export type MarketplaceRecommendRequest = AppModels.MarketplaceRecommendRequest;
export type MarketplaceRecommendResult = AppModels.MarketplaceRecommendResult;
export type MarketplaceRecommendationHistory = AppModels.MarketplaceRecommendationHistory;
export type MarketplaceRecommendationSnapshot = AppModels.MarketplaceRecommendationSnapshot;
export type OpenRegistrationResponse = BindingModels.OpenRegistrationResponse;
export type ProviderEntry = ProviderModels.Entry;
// keep_existing_key is optional here: only the settings import needs it, and the
// generated DTO makes every field required, which would otherwise force three
// unrelated call sites to spell out a flag that does not concern them.
export type SaveProviderInput = Omit<BindingModels.SaveProviderRequest, "keep_existing_key"> & {
  keep_existing_key?: boolean;
};
export type SaveProviderResult = AppModels.SaveProviderResult;
export type ProfileSummary = Omit<AppModels.ProfileSummary, "protocol" | "createdAt" | "context1M"> & {
  protocol: ProtocolId | "";
  createdAt?: string;
  reasoningEffort?: string;
  context1M?: boolean;
};
// Mirrors SaveProviderResult: saving a Profile can rewrite the Agents bound to
// it, so the outcome per Agent travels with the saved record.
export type SaveProfileResult = Omit<AppModels.SaveProfileResult, "profile"> & { profile: ProfileSummary };

export type StatusResponse = Omit<
  AppModels.StatusResponse,
  "platform" | "capabilities" | "agents" | "catalog" | "groups" | "providers" | "mirrors" | "paths" | "backups" | "profiles" | "runtimes" | "desktopAgents"
> & {
	migrationNotice?: string;
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
  desktopAgents: DesktopAgentStatus[];
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
	| { kind: "command"; agent?: string; args: string[] }
	| { kind: "output"; agent?: string; stream: "stdout" | "stderr"; text: string }
	| { kind: "phase"; agent?: string; phase: string }
	| { kind: "source"; agent?: string; target: string; source: string }
  /** total is 0 when the server sent no Content-Length. */
	| { kind: "progress"; agent?: string; target: string; received: number; total: number };

export type InstallRequest = Pick<
  BindingModels.InstallRequest,
  "api_key" | "model" | "configure" | "install_agent" | "skip_test"
> & {
  agents: string[];
  provider: ProviderId;
  api_base_url?: string;
  agent_version?: string;
  registry?: string;
  profile_id?: string;
  profile_label?: string;
  timeout?: number;
};
