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
  platforms: PlatformId[];
  platformNote: string;
}

export interface AgentStatus {
  installed: boolean;
  configured: boolean;
  guideOnly: boolean;
  config: string;
  version: string | null;
  lockedVersion: string | null;
  canInstall: boolean;
}

export interface EnvironmentProfile {
  schema_version: number;
  provider: string;
  base_url: string | null;
  model: string | null;
  config_mode: "provider" | "existing-account";
  agent_ids: string[];
  activated_at: string;
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
  paths: Record<string, string>;
  backups: Record<string, boolean>;
  environment: EnvironmentProfile | null;
  environmentError: string | null;
}

export interface ApiErrorShape {
  ok: false;
  error: string;
  message: string;
  status: number;
  error_code: string;
  retryable: boolean;
}

export interface ProbeResponse {
  ok: boolean;
  reachable: boolean;
  status: number;
  message: string;
  error_code: string | null;
  retryable: boolean;
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
  configure: boolean;
  install_agent: boolean;
  skip_test: boolean;
  locked_version?: boolean;
  latest?: boolean;
  profile_agents?: string[];
}
