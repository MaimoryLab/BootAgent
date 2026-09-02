import { Events, type CancellablePromiseLike } from "@wailsio/runtime";

import * as AgentService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/agentservice.js";
import * as ConversionService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/conversionservice.js";
import * as DesktopAgentService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/desktopagentservice.js";
import * as MarketplaceService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/marketplaceservice.js";
import * as MCPService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/mcpservice.js";
import * as ProfileService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/profileservice.js";
import * as ProviderService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/providerservice.js";
import * as RuntimeService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/runtimeservice.js";
import * as SkillService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/skillservice.js";
import * as StatusService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/statusservice.js";
import * as TaskService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/taskservice.js";
import * as TransferService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/transferservice.js";
import * as UpdateService from "../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/updateservice.js";
import { currentLocale, translate } from "../i18n";
import type {
  ActivateAgentResponse,
  AgentUninstallResult,
  AgentUpdateResult,
  ConversionConfig,
  DesktopAgentActionResult,
  DesktopAgentProfileResult,
  DesktopAgentStatus,
  InstallOutput,
  InstallRequest,
  InstallResponse,
  InstallRuntimeResult,
  LaunchAgentResponse,
  MarketplaceRecommendationAgent,
  MarketplaceDynamicResult,
  MarketplaceRecommendRequest,
  MarketplaceRecommendResult,
  MCPApplyRequest,
  MCPApplyResult,
  MCPScanResult,
  MCPServerDetail,
  MCPServerSummary,
  ModelsResponse,
  OpenRegistrationResponse,
  ProbeResponse,
  ProfileSummary,
  ProviderEntry,
  ProviderId,
  RuntimeStatus,
  SaveProfileResult,
  SaveProviderInput,
  SaveProviderResult,
  Settings,
  SettingsPatch,
  SkillApplyRequest,
  SkillApplyResult,
  SkillBackupSummary,
  SkillImportPreview,
  SkillScanResult,
  SkillSummary,
  SkillUninstallResult,
  StatusResponse,
  TaskHistoryRecord
} from "../types/api";
import type { MarketplaceCatalog, MarketplaceItem } from "../types/marketplace";
import { BootAgentApiError, isCancellationError } from "./errors";

export const INSTALL_OUTPUT_EVENT = "bootagent:install-output";
export const OTA_PROGRESS_TARGET = "bootagent-update";

export function onInstallOutput(listener: (output: InstallOutput) => void): () => void {
  return Events.On(INSTALL_OUTPUT_EVENT, (event) => {
    const data = event.data;
    if (!data || typeof data !== "object") return;
    const kind = (data as { kind?: unknown }).kind;
    if (kind === "command" || kind === "output" || kind === "progress" || kind === "phase" || kind === "source") listener(data as InstallOutput);
  });
}

type ErrorCause = Record<string, unknown>;

export type CancellableRequest<T> = Promise<T> & Partial<Pick<CancellablePromiseLike<T>, "cancel">>;

function causeOf(error: unknown): ErrorCause {
  const cause = error && typeof error === "object" ? (error as { cause?: unknown }).cause : undefined;
  if (typeof cause === "string") {
    try {
      const parsed: unknown = JSON.parse(cause);
      return parsed && typeof parsed === "object" ? (parsed as ErrorCause) : {};
    } catch {
      return {};
    }
  }
  return cause && typeof cause === "object" ? (cause as ErrorCause) : {};
}

function stringValue(value: unknown, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

/** Convert a Wails bridge rejection into the stable frontend error contract. */
export function normalizeWailsError(error: unknown): BootAgentApiError {
  if (error instanceof BootAgentApiError) return error;
  const cause = causeOf(error);
  const known = Object.keys(cause).length > 0;
  return new BootAgentApiError(
    known ? stringValue(cause.message, translate(currentLocale(), "BootAgent 请求失败")) : translate(currentLocale(), "无法调用本机 BootAgent 服务"),
    known ? stringValue(cause.error_code, "INTERNAL_ERROR") : "INTERNAL_ERROR",
    known ? cause.retryable === true : true,
    known ? numberValue(cause.status, 500) : 500,
  );
}

function call<T>(operation: () => PromiseLike<T>): CancellableRequest<T> {
  try {
    return operation().then(
      (value) => value,
      (error) => {
        if (isCancellationError(error)) throw error;
        throw normalizeWailsError(error);
      },
    ) as CancellableRequest<T>;
  } catch (error) {
    return Promise.reject(isCancellationError(error) ? error : normalizeWailsError(error)) as CancellableRequest<T>;
  }
}

const encodeBytes = (data: Uint8Array): string => {
  let binary = "";
  for (let offset = 0; offset < data.length; offset += 0x8000) binary += String.fromCharCode(...data.subarray(offset, offset + 0x8000));
  return btoa(binary);
};

export const wailsApi = {
  onInstallOutput,
  status: (): Promise<StatusResponse> => call(() => StatusService.GetStatus()) as Promise<StatusResponse>,
  checkUpdate: (): Promise<string> => call(() => UpdateService.Check()) as Promise<string>,
  version: (): Promise<string> => call(() => UpdateService.Version()) as Promise<string>,
  // No URL argument: the backend owns it, so a tampered renderer cannot choose
  // what gets opened in the user's browser.
  openHelp: (): Promise<void> => call(() => ProviderService.OpenHelp()).then(() => undefined),
  openGitHub: (): Promise<void> => call(() => ProviderService.OpenGitHub()).then(() => undefined),
  downloadUpdate: (): CancellableRequest<void> => call(() => UpdateService.DownloadAndInstall()) as CancellableRequest<void>,
  restartUpdate: (): Promise<void> => call(() => UpdateService.Restart()).then(() => undefined),
  loadTaskHistory: (): Promise<TaskHistoryRecord[]> => call(() => TaskService.LoadHistory()).then((records) => records ?? []) as Promise<TaskHistoryRecord[]>,
  saveTaskHistory: (records: TaskHistoryRecord[]): Promise<void> => call(() => TaskService.SaveHistory(records)).then(() => undefined),
  desktopAgentStatus: (agentId: string): Promise<DesktopAgentStatus> =>
    call(() => DesktopAgentService.GetStatus({ agent_id: agentId })) as Promise<DesktopAgentStatus>,
  installDesktopAgent: (agentId: string): CancellableRequest<DesktopAgentActionResult> =>
    call(() => DesktopAgentService.Install({ agent_id: agentId })) as CancellableRequest<DesktopAgentActionResult>,
  openDesktopAgent: (agentId: string): Promise<void> =>
    call(() => DesktopAgentService.Open({ agent_id: agentId })).then(() => undefined),
  configureDesktopAgent: (agentId: string, profileId: string): CancellableRequest<DesktopAgentProfileResult> =>
    call(() => DesktopAgentService.Configure({ agent_id: agentId, profile_id: profileId })) as CancellableRequest<DesktopAgentProfileResult>,
  probe: (input: {
    provider: ProviderId;
    apiBaseUrl: string;
    apiKey: string;
    model: string;
    agents?: string[];
    // Set by the Provider editor to test the endpoints and key on screen rather
    // than the stored record. Defaulted here so the wizard's calls keep resolving
    // from storage without naming either field.
    anthropicBaseUrl?: string;
    draft?: boolean;
  }): Promise<ProbeResponse> =>
    call(() => ProviderService.Probe({
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      agents: input.agents?.length ? input.agents : null,
      anthropic_base_url: input.anthropicBaseUrl ?? "",
      draft: input.draft ?? false,
    })) as Promise<ProbeResponse>,
  models: (input: { provider: ProviderId; apiBaseUrl: string; apiKey: string }): Promise<ModelsResponse> =>
    call(() => ProviderService.ListModels({
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
    })) as Promise<ModelsResponse>,
  getProvider: (id: string): Promise<ProviderEntry> =>
    call(() => ProviderService.GetProvider({ id })) as Promise<ProviderEntry>,
  saveProvider: (input: SaveProviderInput): Promise<SaveProviderResult> =>
    // Defaulted here rather than in every caller: false keeps the Provider
    // editor's "empty field clears the key" behaviour, which is what all but the
    // settings import wants.
    call(() => ProviderService.SaveProvider({ keep_existing_key: false, ...input })) as Promise<SaveProviderResult>,
  deleteProvider: (id: string): Promise<void> =>
    call(() => ProviderService.DeleteProvider({ id })).then(() => undefined),
  install: (input: InstallRequest): CancellableRequest<InstallResponse> =>
    call(() => AgentService.Install({
      agents: input.agents,
      provider: input.provider,
      api_base_url: input.api_base_url ?? "",
      api_key: input.api_key,
      model: input.model,
      profile_id: input.profile_id ?? "",
      profile_label: input.profile_label ?? "",
      configure: input.configure,
      install_agent: input.install_agent,
      agent_version: input.agent_version ?? "",
      skip_test: input.skip_test,
      registry: input.registry ?? "",
      // 0 asks Go for its own default. Repeating a number here made this a second
      // source of truth that silently disagreed when the Go side changed.
      timeout: input.timeout ?? 0,
    })) as CancellableRequest<InstallResponse>,
  openRegister: (provider: ProviderId, agents: string[]): Promise<OpenRegistrationResponse> =>
    call(() => ProviderService.OpenRegistration({ provider, agents: agents.length ? agents : null })) as Promise<OpenRegistrationResponse>,
  activateAgent: (
    agentId: string,
    input: { provider: ProviderId; apiBaseUrl: string; apiKey: string; model: string; profileId?: string },
  ): Promise<ActivateAgentResponse> =>
    call(() => AgentService.Activate({
      agent_id: agentId,
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      profile_id: input.profileId ?? "",
    })) as Promise<ActivateAgentResponse>,
  launchAgent: (agentId: string, workingDirectory = ""): Promise<LaunchAgentResponse> =>
    call(() => AgentService.Launch({ agent_id: agentId, working_directory: workingDirectory })) as Promise<LaunchAgentResponse>,
  migrateConversations: (): Promise<import("../types/api").ConversationMigrationResult> =>
    call(() => AgentService.MigrateConversations()) as Promise<import("../types/api").ConversationMigrationResult>,
  uninstallAgent: (agentId: string, allowCrossEnvironment = false, installationId = "", installationIds: string[] = []): CancellableRequest<AgentUninstallResult> =>
    call(() => AgentService.Uninstall({ agent_id: agentId, ...(allowCrossEnvironment ? { allow_cross_environment: true } : {}), ...(installationId ? { installation_id: installationId } : {}), ...(installationIds.length ? { installation_ids: installationIds } : {}) })) as CancellableRequest<AgentUninstallResult>,
  previewUninstall: (agentId: string, installationId = "") =>
    call(() => AgentService.PreviewUninstall({ agent_id: agentId, ...(installationId ? { installation_id: installationId } : {}) })),
  updateAgent: (agentId: string): CancellableRequest<AgentUpdateResult> =>
    call(() => AgentService.Update({ agent_id: agentId })) as CancellableRequest<AgentUpdateResult>,
  listRuntimes: (): Promise<RuntimeStatus[]> =>
    call(() => RuntimeService.ListRuntimes()).then((runtimes) => runtimes ?? []),
  installRuntime: (runtime: string): CancellableRequest<InstallRuntimeResult> =>
    call(() => RuntimeService.InstallRuntime({ runtime })) as CancellableRequest<InstallRuntimeResult>,
  getSettings: (): Promise<Settings> => call(() => RuntimeService.GetSettings()) as Promise<Settings>,
  saveSettings: (settings: SettingsPatch): Promise<Settings> =>
    call(() => RuntimeService.SaveSettings(settings)) as Promise<Settings>,
  getConversion: (): Promise<ConversionConfig> => call(() => ConversionService.Get()) as Promise<ConversionConfig>,
  saveConversion: (config: ConversionConfig): Promise<ConversionConfig> => call(() => ConversionService.Save(config)) as Promise<ConversionConfig>,
  // Marketplace proxy: raw JSON strings from the public skillhub API. The Go
  // side does the GET because api.skillhub.cn only echoes CORS headers for
  // skillhub's own origins; parsing stays with the frontend normalisers.
  marketplaceShowcase: (): Promise<string> =>
    call(() => MarketplaceService.FetchShowcase()).then((response) => response.body),
  marketplaceCatalog: (): Promise<MarketplaceCatalog> =>
    call(() => MarketplaceService.Catalog()).then((catalog) => ({
      version: catalog.version,
      builtAt: catalog.built_at,
      items: (catalog.items ?? []) as MarketplaceItem[],
    })),
  marketplaceDiscoverSources: (options: { source?: string; category?: string; query?: string; limit?: number; offset?: number } = {}): Promise<MarketplaceDynamicResult> =>
    call(() => MarketplaceService.DiscoverSources(options)) as Promise<MarketplaceDynamicResult>,
  marketplaceSkillDetail: (slug: string): Promise<string> =>
    call(() => MarketplaceService.FetchSkillDetail({ slug })).then((response) => response.body),
  marketplaceSkillFile: (slug: string): Promise<string> =>
    call(() => MarketplaceService.FetchSkillFile({ slug })).then((response) => response.body),
  marketplaceRecommendationAgents: (): Promise<MarketplaceRecommendationAgent[]> =>
    call(() => MarketplaceService.RecommendationAgents()).then((agents) => agents ?? []),
  recommendMarketplace: (request: MarketplaceRecommendRequest): Promise<MarketplaceRecommendResult> =>
    call(() => MarketplaceService.Recommend(request)) as Promise<MarketplaceRecommendResult>,
  listRecommendationHistory: (): Promise<import("../types/api").MarketplaceRecommendationHistory[]> =>
    call(() => MarketplaceService.ListRecommendationHistory()).then((records) => records ?? []),
  saveRecommendationHistory: (record: import("../types/api").MarketplaceRecommendationHistory): Promise<import("../types/api").MarketplaceRecommendationHistory> =>
    call(() => MarketplaceService.SaveRecommendationHistory(record)) as Promise<import("../types/api").MarketplaceRecommendationHistory>,
  deleteRecommendationHistory: (id: string): Promise<void> =>
    call(() => MarketplaceService.DeleteRecommendationHistory(id)).then(() => undefined),
  clearRecommendationHistory: (): Promise<void> =>
    call(() => MarketplaceService.ClearRecommendationHistory()).then(() => undefined),
  openMarketplaceExternal: (url: string): Promise<void> =>
    call(() => MarketplaceService.OpenExternal({ url })).then(() => undefined),
  readTransferFile: (): Promise<string> => call(() => TransferService.Read()) as Promise<string>,
  readTransferBytes: (): Promise<Uint8Array> => call(() => TransferService.ReadBytes()).then((data) => Uint8Array.from(atob(data ?? ""), (char) => char.charCodeAt(0))),
  previewTransferV2: (data: Uint8Array): Promise<import("../types/api").TransferV2Preview> => call(() => TransferService.PreviewV2(encodeBytes(data))) as Promise<import("../types/api").TransferV2Preview>,
  applyTransferV2: (data: Uint8Array): Promise<void> => call(() => TransferService.ApplyV2(encodeBytes(data))).then(() => undefined),
  writeTransferFile: (data: string): Promise<void> => call(() => TransferService.Write(data)).then(() => undefined),
  writeTransferBytes: (data: Uint8Array): Promise<void> => call(() => TransferService.WriteBytes(encodeBytes(data))).then(() => undefined),
  exportTransferV2: (providerIDs: string[], profileIDs: string[], mcpIDs: string[], skillIDs: string[]): Promise<Uint8Array> => call(() => TransferService.ExportV2(providerIDs, profileIDs, mcpIDs, skillIDs)).then((data) => Uint8Array.from(atob(data ?? ""), (char) => char.charCodeAt(0))),
  listMCP: (): Promise<MCPServerSummary[]> => call(() => MCPService.List()).then((items) => items ?? []),
  scanMCP: (): Promise<MCPScanResult> => call(() => MCPService.Scan()) as Promise<MCPScanResult>,
  getMCP: (id: string, sourceAgent = ""): Promise<MCPServerDetail> => call(() => MCPService.Get(id, sourceAgent)) as Promise<MCPServerDetail>,
  applyMCP: (request: MCPApplyRequest): Promise<MCPApplyResult> => call(() => MCPService.Apply(request)) as Promise<MCPApplyResult>,
  exportMCP: (mode: string, password = "", confirmPlaintext = false, serverIDs: string[] = []): Promise<string> =>
    call(() => MCPService.Export({ mode, password, confirm_plaintext: confirmPlaintext, server_ids: serverIDs })) as Promise<string>,
  previewImportMCP: (data: string, password = ""): Promise<import("../../bindings/github.com/MaimoryLab/BootAgent/internal/mcp/models.js").Registry> =>
    call(() => MCPService.PreviewImport({ data, password })) as Promise<import("../../bindings/github.com/MaimoryLab/BootAgent/internal/mcp/models.js").Registry>,
  saveImportedMCP: (registry: import("../../bindings/github.com/MaimoryLab/BootAgent/internal/mcp/models.js").Registry): Promise<void> => call(() => MCPService.SaveImported(registry)).then(() => undefined),
  setMCPDraftState: (dirty: boolean, locale: string): Promise<void> => call(() => MCPService.SetDraftState(dirty, locale)).then(() => undefined),
  listSkills: (): Promise<SkillSummary[]> => call(() => SkillService.List()).then((items) => items ?? []),
  exportSkill: (id: string, hash: string): Promise<Uint8Array> => call(() => SkillService.Export(id, hash)).then((data) => Uint8Array.from(atob(data ?? ""), (char) => char.charCodeAt(0))),
  scanSkills: (): Promise<SkillScanResult> => call(() => SkillService.Scan()) as Promise<SkillScanResult>,
  previewSkillImport: (source: string): Promise<SkillImportPreview> => call(() => SkillService.PreviewImport({ source })) as Promise<SkillImportPreview>,
  applySkills: (request: SkillApplyRequest): Promise<SkillApplyResult> => call(() => SkillService.Apply(request)) as Promise<SkillApplyResult>,
  uninstallSkill: (id: string): Promise<SkillUninstallResult> => call(() => SkillService.Uninstall(id)) as Promise<SkillUninstallResult>,
  listSkillBackups: (): Promise<SkillBackupSummary[]> => call(() => SkillService.ListBackups()).then((items) => items ?? []),
  restoreSkillBackup: (backupID: string, targets: string[]): Promise<SkillApplyResult> => call(() => SkillService.RestoreBackup(backupID, targets)) as Promise<SkillApplyResult>,
  setSkillDraftState: (dirty: boolean, locale: string): Promise<void> => call(() => SkillService.SetDraftState(dirty, locale)).then(() => undefined),
  listProfiles: (): Promise<ProfileSummary[]> => call(() => ProfileService.ListProfiles()) as Promise<ProfileSummary[]>,
  deleteProfile: (id: string): Promise<void> =>
    call(() => ProfileService.DeleteProfile({ id })).then(() => undefined),
  saveProfile: (input: {
    id: string;
    label: string;
    provider: ProviderId;
    apiBaseUrl: string;
    apiKey: string;
    model: string;
    reasoningEffort?: string;
    context1M?: boolean;
    configMode: string;
    protocol?: string;
  }): CancellableRequest<SaveProfileResult> =>
    call(() => ProfileService.SaveProfile({
      id: input.id,
      label: input.label,
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      reasoning_effort: input.reasoningEffort ?? "",
      context_1m: input.context1M ?? false,
      config_mode: input.configMode,
      protocol: input.protocol ?? "",
    })) as CancellableRequest<SaveProfileResult>,
};
