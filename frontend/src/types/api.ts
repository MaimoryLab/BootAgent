export type PlatformId = "macos" | "windows" | "linux";
export type AgentGroupId = "auto" | "gateway" | "platform" | "ide";
export type ProviderId = "ppio" | "novita" | "custom";

export interface AgentCatalogItem {
  id: string;
  name: string;
  group: AgentGroupId;
  configMode: "auto" | "guide";
  guideOnly: boolean;
  lockedVersion: string | null;
  /** Inference protocol this Agent speaks; null for guide-only Agents. */
  protocol: ProtocolId | null;
  platforms: PlatformId[];
  platformNote: string;
  /** Display prominence; lower sorts first. Independent of configMode. */
  rank: number;
}

export interface AgentStatus {
  installed: boolean;
  configured: boolean;
  guideOnly: boolean;
  config: string;
  version: string | null;
  lockedVersion: string | null;
  canInstall: boolean;
  /** What this Agent is pointed at. Null until it has been configured once. */
  provider: string | null;
  model: string | null;
  baseUrl: string | null;
  updatedAt: string | null;
}

export interface ActivateAgentResponse {
  ok: boolean;
  agent: string;
  config: string;
  provider: string;
  model: string;
  /** How to make the rewritten config take effect; Agents read it at startup. */
  restart: string;
  next: string;
}

export interface EnvironmentProfile {
  schema_version: number;
  id?: string;
  label?: string;
  provider: string;
  base_url: string | null;
  model: string | null;
  config_mode: "provider" | "existing-account";
  agent_ids: string[];
  activated_at: string;
  created_at?: string;
}

export interface ProfileSummary {
  id: string;
  label: string;
  provider: string;
  baseUrl: string | null;
  model: string | null;
  agentIds: string[];
  activatedAt: string | null;
  hasKey: boolean;
}

export interface StatusResponse {
  apiVersion: number;
  platform: { os: PlatformId; arch: string; shell: string };
  capabilities: {
    canInstall: Record<string, boolean>;
    supportedAgentIds: string[];
  };
  agents: Record<string, AgentStatus>;
  catalog: AgentCatalogItem[];
  groups: Array<{ id: AgentGroupId; name: string }>;
  providers: Record<string, { name: string; home: string; base_url: string; anthropic_base_url?: string }>;
  /**
   * Package registries the user may install from. The official one is the
   * default; a mirror is only ever an explicit choice, and `upstream` is carried
   * so the UI can show where a package ultimately comes from.
   */
  mirrors: Array<{ id: string; name: string; registry: string; upstream: string; note: string }>;
  paths: Record<string, string>;
  backups: Record<string, boolean>;
  environment: EnvironmentProfile | null;
  environmentError: string | null;
  profiles: ProfileSummary[];
  activeProfile: string | null;
}

export interface ApiErrorShape {
  ok: false;
  error: string;
  message: string;
  status: number;
  error_code: string;
  retryable: boolean;
}

export type ProtocolId = "openai" | "anthropic" | "responses";

export const PROTOCOL_LABELS: Record<ProtocolId, string> = {
  openai: "OpenAI Chat Completions",
  anthropic: "Anthropic Messages",
  responses: "OpenAI Responses",
};

export interface ProbeResponse {
  ok: boolean;
  reachable: boolean;
  status: number;
  message: string;
  error_code: string | null;
  retryable: boolean;
  /** Which protocol this result describes. Absent on pre-protocol responses. */
  protocol?: ProtocolId;
  /** One entry per protocol the selected Agents speak. */
  protocols?: Partial<Record<ProtocolId, ProbeResponse>>;
}

export interface ModelsResponse extends ProbeResponse {
  models: string[];
}

export type AgentResultStatus = "configured" | "guide-only" | "installed" | "skipped" | "failed";

export interface AgentInstallResult {
  agent: string;
  status: AgentResultStatus;
  installed?: boolean;
  version?: string | null;
  lockedVersion?: string | null;
  config?: string;
  code?: number;
  error_code?: string;
  message?: string;
  retryable: boolean;
}

export interface InstallResponse {
  ok: boolean;
  code: number;
  results: AgentInstallResult[];
  log: string;
  next: string;
  probe: ProbeResponse | null;
}

export interface ProviderInput {
  provider: ProviderId;
  api_base_url?: string;
  api_key: string;
}

export interface InstallRequest extends ProviderInput {
  agents: string[];
  model: string;
  /** Claude Code only: a cheaper model for fast/background work. Empty follows
   *  `model`; the backend ignores it for every other adapter. */
  small_fast_model?: string;
  configure: boolean;
  install_agent: boolean;
  skip_test: boolean;
  locked_version?: boolean;
  latest?: boolean;
  profile_agents?: string[];
  /** Mirror id or https:// URL. Omit for the official registry. */
  registry?: string;
}
