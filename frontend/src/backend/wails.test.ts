import { afterEach, describe, expect, it, vi } from "vitest";

import type { DesktopAgentActionResult, DesktopAgentProfileResult, DesktopAgentStatus, InstallResponse, ModelsResponse, ProbeResponse, ProfileSummary, ProviderEntry, StatusResponse } from "../types/api";
import { LOCALE_STORAGE_KEY } from "../i18n";

const bridge = vi.hoisted(() => ({
  status: vi.fn(),
  probe: vi.fn(),
  models: vi.fn(),
  getProvider: vi.fn(),
  saveProvider: vi.fn(),
  deleteProvider: vi.fn(),
  install: vi.fn(),
  register: vi.fn(),
  activate: vi.fn(),
  launch: vi.fn(),
  desktopStatus: vi.fn(),
  desktopInstall: vi.fn(),
  desktopOpen: vi.fn(),
  desktopInstaller: vi.fn(),
  desktopConfigure: vi.fn(),
  profiles: vi.fn(),
  saveProfile: vi.fn(),
  eventsOn: vi.fn(),
}));

vi.mock("@wailsio/runtime", () => ({ Events: { On: bridge.eventsOn } }));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/statusservice.js", () => ({ GetStatus: bridge.status }));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/providerservice.js", () => ({
  Probe: bridge.probe,
  ListModels: bridge.models,
  OpenRegistration: bridge.register,
  GetProvider: bridge.getProvider,
  SaveProvider: bridge.saveProvider,
  DeleteProvider: bridge.deleteProvider,
}));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/agentservice.js", () => ({
  Install: bridge.install,
  Activate: bridge.activate,
  Launch: bridge.launch,
}));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/desktopagentservice.js", () => ({
  GetStatus: bridge.desktopStatus,
  Install: bridge.desktopInstall,
  Open: bridge.desktopOpen,
  OpenInstaller: bridge.desktopInstaller,
  Configure: bridge.desktopConfigure,
}));
vi.mock("../../bindings/github.com/MaimoryLab/OneAgent/internal/binding/profileservice.js", () => ({
  ListProfiles: bridge.profiles,
  SaveProfile: bridge.saveProfile,
}));

import { INSTALL_OUTPUT_EVENT, normalizeWailsError, onInstallOutput, wailsApi } from "./wails";

describe("Wails backend adapter", () => {
  afterEach(() => vi.resetAllMocks());

  it("forwards the page-facing calls to generated bindings", async () => {
    const status = {
      apiVersion: 1,
      platform: { os: "linux", arch: "arm64", shell: "bash" },
      runtimes: [],
      capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
      agents: {}, catalog: [], groups: [], providers: {}, mirrors: [], paths: {}, backups: {},
      profiles: [], activeProfile: null, environment: null, environmentError: null, firstRun: false,
      desktopAgent: { id: "desktop-agent", name: "ChatGPT Desktop", installed: false, supported: false, version: null, source: "unknown" },
    } satisfies StatusResponse;
    const probe = { ok: true, reachable: true, status: 204, message: "ok", error_code: null, retryable: false } satisfies ProbeResponse;
    const models = { ...probe, models: ["model-a"] } satisfies ModelsResponse;
    const install = { ok: true, code: 0, results: [], log: "", next: "", probe: null } satisfies InstallResponse;
    const profile = { id: "team", label: "Team", provider: "ppio", baseUrl: null, model: "m", protocol: "responses", activatedAt: null, hasKey: true } satisfies ProfileSummary;
    const provider = { id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test", anthropic_base_url: "", api_key: "secret", built_in: false } satisfies ProviderEntry;
    const desktopStatus = { id: "desktop-agent", name: "ChatGPT Desktop", installed: false, supported: true, version: null, source: "macos-dmg", profileAgentId: "codex", profileId: null } satisfies DesktopAgentStatus;
    const desktopAction = { status: "installer-started", message: "started", refreshNeeded: true, app: desktopStatus } satisfies DesktopAgentActionResult;
    const desktopProfile = { agent: "desktop-agent", profileId: "team", profileAgentId: "codex", config: "/c", restart: "restart", message: "applied" } satisfies DesktopAgentProfileResult;

    bridge.status.mockResolvedValue(status);
    bridge.probe.mockResolvedValue(probe);
    bridge.models.mockResolvedValue(models);
    bridge.install.mockResolvedValue(install);
    bridge.register.mockResolvedValue({ ok: true, url: "https://ppio.com/", message: "opened" });
    bridge.activate.mockResolvedValue({ ok: true, agent: "codex", config: "/c", provider: "ppio", model: "m", restart: "restart", next: "next" });
    bridge.launch.mockResolvedValue({ ok: true, agent: "codex", command: "codex" });
    bridge.profiles.mockResolvedValue([profile]);
    bridge.saveProfile.mockResolvedValue(profile);
    bridge.getProvider.mockResolvedValue(provider);
    bridge.saveProvider.mockResolvedValue(provider);
    bridge.deleteProvider.mockResolvedValue({ ok: true });
    bridge.desktopStatus.mockResolvedValue(desktopStatus);
    bridge.desktopInstall.mockResolvedValue(desktopAction);
    bridge.desktopOpen.mockResolvedValue(undefined);
    bridge.desktopInstaller.mockResolvedValue(desktopAction);
    bridge.desktopConfigure.mockResolvedValue(desktopProfile);

    await expect(wailsApi.status()).resolves.toBe(status);
    await expect(wailsApi.desktopAgentStatus()).resolves.toBe(desktopStatus);
    await expect(wailsApi.installDesktopAgent()).resolves.toBe(desktopAction);
    await expect(wailsApi.openDesktopAgent()).resolves.toBeUndefined();
    await expect(wailsApi.openDesktopAgentInstaller()).resolves.toBe(desktopAction);
    await expect(wailsApi.configureDesktopAgent("desktop-agent", "team")).resolves.toBe(desktopProfile);
    await expect(wailsApi.probe({ provider: "custom", apiBaseUrl: "https://proxy.test/v1", apiKey: "secret", model: "m", agents: [] })).resolves.toBe(probe);
    await expect(wailsApi.models({ provider: "ppio", apiBaseUrl: "", apiKey: "secret" })).resolves.toBe(models);
    await expect(wailsApi.getProvider("acme")).resolves.toBe(provider);
    await expect(wailsApi.saveProvider({ id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test", anthropic_base_url: "", api_key: "secret" })).resolves.toBe(provider);
    await expect(wailsApi.deleteProvider("acme")).resolves.toBeUndefined();
    await expect(wailsApi.install({ agents: ["codex"], provider: "ppio", api_key: "secret", model: "m", configure: true, install_agent: false, skip_test: true })).resolves.toBe(install);
    await wailsApi.openRegister("ppio", []);
    await wailsApi.activateAgent("codex", { provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m" });
    await wailsApi.launchAgent("codex");
    await expect(wailsApi.listProfiles()).resolves.toEqual([profile]);
    await expect(wailsApi.saveProfile({ id: "team", label: "Team", provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m", configMode: "provider" })).resolves.toBe(profile);

    expect(bridge.probe).toHaveBeenCalledWith({ provider: "custom", api_base_url: "https://proxy.test/v1", api_key: "secret", model: "m", agents: null });
    expect(bridge.getProvider).toHaveBeenCalledWith({ id: "acme" });
    expect(bridge.deleteProvider).toHaveBeenCalledWith({ id: "acme" });
    expect(bridge.install).toHaveBeenCalledWith(expect.objectContaining({ agents: ["codex"], timeout: 180, agent_version: "" }));
    expect(bridge.register).toHaveBeenCalledWith({ provider: "ppio", agents: null });
    expect(bridge.activate).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "codex", profile_id: "", small_fast_model: "" }));
    expect(bridge.launch).toHaveBeenCalledWith({ agent_id: "codex" });
    expect(bridge.desktopStatus).toHaveBeenCalledWith();
    expect(bridge.desktopInstall).toHaveBeenCalledWith();
    expect(bridge.desktopOpen).toHaveBeenCalledWith();
    expect(bridge.desktopInstaller).toHaveBeenCalledWith();
    expect(bridge.desktopConfigure).toHaveBeenCalledWith({ agent_id: "desktop-agent", profile_id: "team" });
    expect(bridge.saveProfile).toHaveBeenCalledWith(expect.objectContaining({ api_base_url: "", api_key: "secret" }));
  });

  it("restores structured Wails errors without exposing raw bridge details", async () => {
    expect(normalizeWailsError({ cause: { error_code: "API_KEY_REJECTED", message: "key rejected", status: 401, retryable: false } })).toMatchObject({
      message: "key rejected", code: "API_KEY_REJECTED", status: 401, retryable: false,
    });
    expect(normalizeWailsError({ cause: '{"error_code":"TIMEOUT","message":"probe timed out","status":504,"retryable":true}' })).toMatchObject({
      message: "probe timed out", code: "TIMEOUT", status: 504, retryable: true,
    });
    localStorage.setItem(LOCALE_STORAGE_KEY, "zh-CN");
    expect(normalizeWailsError(new Error("secret-key-value"))).toMatchObject({
      message: "无法调用本机 OneAgent 服务", code: "INTERNAL_ERROR", status: 500, retryable: true,
    });
  });

  it("subscribes to and filters installation output events", () => {
    const unsubscribe = vi.fn();
    const listener = vi.fn();
    bridge.eventsOn.mockImplementation((_name, callback) => {
      callback({ data: { kind: "command", args: ["npm"] } });
      callback({ data: { kind: "output", stream: "stdout", text: "ready" } });
      callback({ data: null });
      callback({ data: "ignored" });
      callback({ data: { kind: "other" } });
      return unsubscribe;
    });

    expect(onInstallOutput(listener)).toBe(unsubscribe);
    expect(bridge.eventsOn).toHaveBeenCalledWith(INSTALL_OUTPUT_EVENT, expect.any(Function));
    expect(listener).toHaveBeenCalledTimes(2);
  });
});
