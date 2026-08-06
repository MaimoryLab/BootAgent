import { Events, type CancellablePromiseLike } from "@wailsio/runtime";

import * as AgentService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/agentservice.js";
import * as DesktopAgentService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/desktopagentservice.js";
import * as ProfileService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/profileservice.js";
import * as ProviderService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/providerservice.js";
import * as RuntimeService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/runtimeservice.js";
import * as StatusService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/statusservice.js";
import * as UpdateService from "../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/updateservice.js";
import type {
  ActivateAgentResponse,
  DesktopAgentActionResult,
  DesktopAgentProfileResult,
  DesktopAgentStatus,
  InstallRequest,
  InstallOutput,
  InstallResponse,
  InstallRuntimeResult,
  LaunchAgentResponse,
  AgentUpdateResult,
  ModelsResponse,
  OpenRegistrationResponse,
  ProbeResponse,
  ProviderEntry,
  ProfileSummary,
  ProviderId,
  RuntimeStatus,
  SaveProviderInput,
  SaveProviderResult,
  Settings,
  StatusResponse,
} from "../types/api";
import { currentLocale, translate } from "../i18n";
import { isCancellationError, OneAgentApiError } from "./errors";

export const INSTALL_OUTPUT_EVENT = "oneagent:install-output";
export const OTA_PROGRESS_TARGET = "oneagent-update";

export function onInstallOutput(listener: (output: InstallOutput) => void): () => void {
  return Events.On(INSTALL_OUTPUT_EVENT, (event) => {
    const data = event.data;
    if (!data || typeof data !== "object") return;
    const kind = (data as { kind?: unknown }).kind;
    if (kind === "command" || kind === "output" || kind === "progress") listener(data as InstallOutput);
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
export function normalizeWailsError(error: unknown): OneAgentApiError {
  if (error instanceof OneAgentApiError) return error;
  const cause = causeOf(error);
  const known = Object.keys(cause).length > 0;
  return new OneAgentApiError(
    known ? stringValue(cause.message, translate(currentLocale(), "OneAgent 请求失败")) : translate(currentLocale(), "无法调用本机 OneAgent 服务"),
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

export const wailsApi = {
  onInstallOutput,
  status: (): Promise<StatusResponse> => call(() => StatusService.GetStatus()) as Promise<StatusResponse>,
  checkUpdate: (): Promise<string> => call(() => UpdateService.Check()) as Promise<string>,
  downloadUpdate: (): CancellableRequest<void> => call(() => UpdateService.DownloadAndInstall()) as CancellableRequest<void>,
  restartUpdate: (): Promise<void> => call(() => UpdateService.Restart()).then(() => undefined),
  desktopAgentStatus: (agentId: string): Promise<DesktopAgentStatus> =>
    call(() => DesktopAgentService.GetStatus({ agent_id: agentId })) as Promise<DesktopAgentStatus>,
  installDesktopAgent: (agentId: string): CancellableRequest<DesktopAgentActionResult> =>
    call(() => DesktopAgentService.Install({ agent_id: agentId })) as CancellableRequest<DesktopAgentActionResult>,
  openDesktopAgent: (agentId: string): Promise<void> =>
    call(() => DesktopAgentService.Open({ agent_id: agentId })).then(() => undefined),
  configureDesktopAgent: (agentId: string, profileId: string): CancellableRequest<DesktopAgentProfileResult> =>
    call(() => DesktopAgentService.Configure({ agent_id: agentId, profile_id: profileId })) as CancellableRequest<DesktopAgentProfileResult>,
  probe: (input: { provider: ProviderId; apiBaseUrl: string; apiKey: string; model: string; agents?: string[] }): Promise<ProbeResponse> =>
    call(() => ProviderService.Probe({
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      agents: input.agents?.length ? input.agents : null,
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
    call(() => ProviderService.SaveProvider(input)) as Promise<SaveProviderResult>,
  deleteProvider: (id: string): Promise<void> =>
    call(() => ProviderService.DeleteProvider({ id })).then(() => undefined),
  install: (input: InstallRequest): CancellableRequest<InstallResponse> =>
    call(() => AgentService.Install({
      agents: input.agents,
      provider: input.provider,
      api_base_url: input.api_base_url ?? "",
      api_key: input.api_key,
      model: input.model,
      small_fast_model: input.small_fast_model ?? "",
      profile_id: input.profile_id ?? "",
      profile_label: input.profile_label ?? "",
      configure: input.configure,
      install_agent: input.install_agent,
      agent_version: input.agent_version ?? "",
      skip_test: input.skip_test,
      registry: input.registry ?? "",
      timeout: input.timeout ?? 180,
    })) as CancellableRequest<InstallResponse>,
  openRegister: (provider: ProviderId, agents: string[]): Promise<OpenRegistrationResponse> =>
    call(() => ProviderService.OpenRegistration({ provider, agents: agents.length ? agents : null })) as Promise<OpenRegistrationResponse>,
  activateAgent: (
    agentId: string,
    input: { provider: ProviderId; apiBaseUrl: string; apiKey: string; model: string; profileId?: string; smallFastModel?: string },
  ): Promise<ActivateAgentResponse> =>
    call(() => AgentService.Activate({
      agent_id: agentId,
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      profile_id: input.profileId ?? "",
      small_fast_model: input.smallFastModel ?? "",
    })) as Promise<ActivateAgentResponse>,
  launchAgent: (agentId: string): Promise<LaunchAgentResponse> =>
    call(() => AgentService.Launch({ agent_id: agentId })) as Promise<LaunchAgentResponse>,
  updateAgent: (agentId: string): CancellableRequest<AgentUpdateResult> =>
    call(() => AgentService.Update({ agent_id: agentId })) as CancellableRequest<AgentUpdateResult>,
  listRuntimes: (): Promise<RuntimeStatus[]> =>
    call(() => RuntimeService.ListRuntimes()).then((runtimes) => runtimes ?? []),
  installRuntime: (runtime: string): CancellableRequest<InstallRuntimeResult> =>
    call(() => RuntimeService.InstallRuntime({ runtime })) as CancellableRequest<InstallRuntimeResult>,
  getSettings: (): Promise<Settings> => call(() => RuntimeService.GetSettings()) as Promise<Settings>,
  saveSettings: (settings: Settings): Promise<Settings> =>
    call(() => RuntimeService.SaveSettings(settings)) as Promise<Settings>,
  listProfiles: (): Promise<ProfileSummary[]> => call(() => ProfileService.ListProfiles()) as Promise<ProfileSummary[]>,
  saveProfile: (input: {
    id: string;
    label: string;
    provider: ProviderId;
    apiBaseUrl: string;
    apiKey: string;
    model: string;
    configMode: string;
    protocol?: string;
  }): CancellableRequest<ProfileSummary> =>
    call(() => ProfileService.SaveProfile({
      id: input.id,
      label: input.label,
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      config_mode: input.configMode,
      protocol: input.protocol ?? "",
    })) as CancellableRequest<ProfileSummary>,
};
