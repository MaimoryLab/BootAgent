import { afterEach, describe, expect, it, vi } from "vitest";

import { LOCALE_STORAGE_KEY } from "../i18n";
import type { DesktopAgentActionResult, DesktopAgentProfileResult, DesktopAgentStatus, InstallResponse, ModelsResponse, ProbeResponse, ProfileSummary, ProviderEntry, StatusResponse } from "../types/api";

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
  migrateConversations: vi.fn(),
  desktopStatus: vi.fn(),
  desktopInstall: vi.fn(),
  desktopOpen: vi.fn(),
  desktopConfigure: vi.fn(),
  profiles: vi.fn(),
  deleteProfile: vi.fn(),
  saveProfile: vi.fn(),
  updateCheck: vi.fn(),
  updateDownload: vi.fn(),
  updateRestart: vi.fn(),
  eventsOn: vi.fn(),
  readTransfer: vi.fn(),
  writeTransfer: vi.fn(),
}));

vi.mock("@wailsio/runtime", async (importOriginal) => ({
  ...await (async () => {
    const domWindow = globalThis.window;
    vi.stubGlobal("window", undefined);
    try {
      return await importOriginal<typeof import("@wailsio/runtime")>();
    } finally {
      vi.stubGlobal("window", domWindow);
    }
  })(),
  Events: { On: bridge.eventsOn },
}));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/statusservice.js", () => ({ GetStatus: bridge.status }));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/providerservice.js", () => ({
  Probe: bridge.probe,
  ListModels: bridge.models,
  OpenRegistration: bridge.register,
  GetProvider: bridge.getProvider,
  SaveProvider: bridge.saveProvider,
  DeleteProvider: bridge.deleteProvider,
}));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/agentservice.js", () => ({
  Install: bridge.install,
  Activate: bridge.activate,
  Launch: bridge.launch,
  MigrateConversations: bridge.migrateConversations,
}));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/desktopagentservice.js", () => ({
  GetStatus: bridge.desktopStatus,
  Install: bridge.desktopInstall,
  Open: bridge.desktopOpen,
  Configure: bridge.desktopConfigure,
}));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/profileservice.js", () => ({
  ListProfiles: bridge.profiles,
  DeleteProfile: bridge.deleteProfile,
  SaveProfile: bridge.saveProfile,
}));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/updateservice.js", () => ({
  Check: bridge.updateCheck,
  DownloadAndInstall: bridge.updateDownload,
  Restart: bridge.updateRestart,
}));
vi.mock("../../bindings/github.com/MaimoryLab/BootAgent/internal/binding/transferservice.js", () => ({
  Read: bridge.readTransfer,
  Write: bridge.writeTransfer,
}));

import { CancellablePromise } from "@wailsio/runtime";
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
      desktopAgents: [],
    } satisfies StatusResponse;
    const probe = { ok: true, reachable: true, status: 204, message: "ok", error_code: null, retryable: false } satisfies ProbeResponse;
    const models = { ...probe, models: ["model-a"] } satisfies ModelsResponse;
    const install = { ok: true, code: 0, results: [], log: "", next: "", probe: null } satisfies InstallResponse;
    const profile = { id: "team", label: "Team", provider: "ppio", baseUrl: null, model: "m", protocol: "responses", activatedAt: null } satisfies ProfileSummary;
    const provider = { id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test", anthropic_base_url: "", api_key: "secret", built_in: false } satisfies ProviderEntry;
    const desktopStatus = { id: "chatgpt-desktop", name: "ChatGPT Desktop", installed: false, supported: true, version: null, source: "macos-dmg", protocol: "responses", profileAgentId: "codex", profileId: null } satisfies DesktopAgentStatus;
    const desktopAction = { status: "installer-started", message: "started", refreshNeeded: true, app: desktopStatus } satisfies DesktopAgentActionResult;
    const desktopProfile = { agent: "chatgpt-desktop", profileId: "team", profileAgentId: "codex", config: "/c", restart: "restart", message: "applied" } satisfies DesktopAgentProfileResult;

    bridge.status.mockResolvedValue(status);
    bridge.probe.mockResolvedValue(probe);
    bridge.models.mockResolvedValue(models);
    bridge.install.mockResolvedValue(install);
    bridge.register.mockResolvedValue({ ok: true, url: "https://ppio.com/", message: "opened" });
    bridge.activate.mockResolvedValue({ ok: true, agent: "codex", config: "/c", provider: "ppio", model: "m", restart: "restart", next: "next" });
    bridge.launch.mockResolvedValue({ ok: true, agent: "codex", command: "codex" });
    bridge.migrateConversations.mockResolvedValue({ files: 2, threads: 3 });
    bridge.profiles.mockResolvedValue([profile]);
    bridge.deleteProfile.mockResolvedValue({ ok: true });
    bridge.saveProfile.mockResolvedValue(profile);
    bridge.getProvider.mockResolvedValue(provider);
    bridge.saveProvider.mockResolvedValue(provider);
    bridge.deleteProvider.mockResolvedValue({ ok: true });
    bridge.desktopStatus.mockResolvedValue(desktopStatus);
    bridge.desktopInstall.mockResolvedValue(desktopAction);
    bridge.desktopOpen.mockResolvedValue(undefined);
    bridge.desktopConfigure.mockResolvedValue(desktopProfile);
    bridge.readTransfer.mockResolvedValue("contents");
    bridge.writeTransfer.mockResolvedValue(undefined);

    await expect(wailsApi.status()).resolves.toBe(status);
    await expect(wailsApi.desktopAgentStatus("chatgpt-desktop")).resolves.toBe(desktopStatus);
    await expect(wailsApi.installDesktopAgent("chatgpt-desktop")).resolves.toBe(desktopAction);
    await expect(wailsApi.openDesktopAgent("chatgpt-desktop")).resolves.toBeUndefined();
    await expect(wailsApi.configureDesktopAgent("chatgpt-desktop", "team")).resolves.toBe(desktopProfile);
    await expect(wailsApi.probe({ provider: "custom", apiBaseUrl: "https://proxy.test/v1", apiKey: "secret", model: "m", agents: [] })).resolves.toBe(probe);
    await expect(wailsApi.models({ provider: "ppio", apiBaseUrl: "", apiKey: "secret" })).resolves.toBe(models);
    await expect(wailsApi.getProvider("acme")).resolves.toBe(provider);
    await expect(wailsApi.saveProvider({ id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test", anthropic_base_url: "", api_key: "secret", create: true })).resolves.toBe(provider);
    await expect(wailsApi.deleteProvider("acme")).resolves.toBeUndefined();
    await expect(wailsApi.install({ agents: ["codex"], provider: "ppio", api_key: "secret", model: "m", configure: true, install_agent: false, skip_test: true })).resolves.toBe(install);
    await wailsApi.openRegister("ppio", []);
    await wailsApi.activateAgent("codex", { provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m" });
    await wailsApi.launchAgent("codex");
    await expect(wailsApi.migrateConversations()).resolves.toEqual({ files: 2, threads: 3 });
    await expect(wailsApi.listProfiles()).resolves.toEqual([profile]);
    await expect(wailsApi.deleteProfile("team")).resolves.toBeUndefined();
    await expect(wailsApi.saveProfile({ id: "team", label: "Team", provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m", configMode: "provider" })).resolves.toBe(profile);
    await expect(wailsApi.readTransferFile()).resolves.toBe("contents");
    await expect(wailsApi.writeTransferFile("contents")).resolves.toBeUndefined();

    // The wizard names neither new field, so both must default: draft false keeps
    // it resolving the stored Provider, which is what it relies on.
    expect(bridge.probe).toHaveBeenCalledWith({ provider: "custom", api_base_url: "https://proxy.test/v1", api_key: "secret", model: "m", agents: null, anthropic_base_url: "", draft: false });
    expect(bridge.getProvider).toHaveBeenCalledWith({ id: "acme" });
    expect(bridge.deleteProvider).toHaveBeenCalledWith({ id: "acme" });
    // 0 means "use the Go default" rather than a duplicated number here.
    expect(bridge.install).toHaveBeenCalledWith(expect.objectContaining({ agents: ["codex"], timeout: 0, agent_version: "" }));
    expect(bridge.register).toHaveBeenCalledWith({ provider: "ppio", agents: null });
    expect(bridge.activate).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "codex", profile_id: "", small_fast_model: "" }));
    expect(bridge.launch).toHaveBeenCalledWith({ agent_id: "codex", working_directory: "" });
    expect(bridge.desktopStatus).toHaveBeenCalledWith({ agent_id: "chatgpt-desktop" });
    expect(bridge.desktopInstall).toHaveBeenCalledWith({ agent_id: "chatgpt-desktop" });
    expect(bridge.desktopOpen).toHaveBeenCalledWith({ agent_id: "chatgpt-desktop" });
    expect(bridge.desktopConfigure).toHaveBeenCalledWith({ agent_id: "chatgpt-desktop", profile_id: "team" });
    expect(bridge.saveProfile).toHaveBeenCalledWith(expect.objectContaining({ api_base_url: "", api_key: "secret" }));
    expect(bridge.deleteProfile).toHaveBeenCalledWith({ id: "team" });
    expect(bridge.readTransfer).toHaveBeenCalledWith();
    expect(bridge.writeTransfer).toHaveBeenCalledWith("contents");
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
      message: "无法调用本机 BootAgent 服务", code: "INTERNAL_ERROR", status: 500, retryable: true,
    });
  });

  it("preserves Wails cancellation through the error adapter", async () => {
    const oncancelled = vi.fn();
    bridge.install.mockReturnValue(new CancellablePromise<InstallResponse>(() => {}, oncancelled));
    const request = wailsApi.install({ agents: ["codex"], provider: "ppio", api_key: "", model: "m", configure: true, install_agent: true, skip_test: true });
    const rejection = request.catch((error) => error);

    expect(typeof request.cancel).toBe("function");
    await request.cancel?.();
    expect(oncancelled).toHaveBeenCalledOnce();
    await expect(rejection).resolves.toMatchObject({ name: "CancelError" });
  });

  it("forwards OTA calls and preserves download cancellation", async () => {
    const oncancelled = vi.fn();
    bridge.updateCheck.mockResolvedValue("1.2.3");
    bridge.updateDownload.mockReturnValue(new CancellablePromise<void>(() => {}, oncancelled));
    bridge.updateRestart.mockResolvedValue(undefined);

    await expect(wailsApi.checkUpdate()).resolves.toBe("1.2.3");
    const request = wailsApi.downloadUpdate();
    expect(typeof request.cancel).toBe("function");
    await request.cancel?.();
    await expect(wailsApi.restartUpdate()).resolves.toBeUndefined();
    expect(oncancelled).toHaveBeenCalledOnce();
    expect(bridge.updateCheck).toHaveBeenCalledWith();
    expect(bridge.updateDownload).toHaveBeenCalledWith();
    expect(bridge.updateRestart).toHaveBeenCalledWith();
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
