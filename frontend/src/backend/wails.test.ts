import { afterEach, describe, expect, it, vi } from "vitest";

import type { InstallResponse, ModelsResponse, ProbeResponse, ProfileSummary, ProviderEntry, StatusResponse } from "../types/api";

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
      capabilities: { canInstall: {}, supportedAgentIds: [] },
      agents: {}, catalog: [], groups: [], providers: {}, mirrors: [], paths: {}, backups: {},
      profiles: [], activeProfile: null, environment: null, environmentError: null,
    } satisfies StatusResponse;
    const probe = { ok: true, reachable: true, status: 204, message: "ok", error_code: null, retryable: false } satisfies ProbeResponse;
    const models = { ...probe, models: ["model-a"] } satisfies ModelsResponse;
    const install = { ok: true, code: 0, results: [], log: "", next: "", probe: null } satisfies InstallResponse;
    const profile = { id: "team", label: "Team", provider: "ppio", baseUrl: null, model: "m", agentIds: ["codex"], activatedAt: null, hasKey: true } satisfies ProfileSummary;
    const provider = { id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test", anthropic_base_url: "", api_key: "secret", built_in: false } satisfies ProviderEntry;

    bridge.status.mockResolvedValue(status);
    bridge.probe.mockResolvedValue(probe);
    bridge.models.mockResolvedValue(models);
    bridge.install.mockResolvedValue(install);
    bridge.register.mockResolvedValue({ ok: true, url: "https://ppio.com/", message: "opened" });
    bridge.activate.mockResolvedValue({ ok: true, agent: "codex", config: "/c", provider: "ppio", model: "m", restart: "restart", next: "next" });
    bridge.profiles.mockResolvedValue([profile]);
    bridge.saveProfile.mockResolvedValue(profile);
    bridge.getProvider.mockResolvedValue(provider);
    bridge.saveProvider.mockResolvedValue(provider);
    bridge.deleteProvider.mockResolvedValue({ ok: true });

    await expect(wailsApi.status()).resolves.toBe(status);
    await expect(wailsApi.probe({ provider: "custom", apiBaseUrl: "https://proxy.test/v1", apiKey: "secret", model: "m", agents: [] })).resolves.toBe(probe);
    await expect(wailsApi.models({ provider: "ppio", apiBaseUrl: "", apiKey: "secret" })).resolves.toBe(models);
    await expect(wailsApi.getProvider("acme")).resolves.toBe(provider);
    await expect(wailsApi.saveProvider({ id: "acme", name: "Acme", home: "", base_url: "https://api.acme.test", anthropic_base_url: "", api_key: "secret" })).resolves.toBe(provider);
    await expect(wailsApi.deleteProvider("acme")).resolves.toBeUndefined();
    await expect(wailsApi.install({ agents: ["codex"], provider: "ppio", api_key: "secret", model: "m", configure: true, install_agent: false, skip_test: true })).resolves.toBe(install);
    await wailsApi.openRegister("ppio", []);
    await wailsApi.activateAgent("codex", { provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m" });
    await expect(wailsApi.listProfiles()).resolves.toEqual([profile]);
    await expect(wailsApi.saveProfile({ id: "team", label: "Team", provider: "ppio", apiBaseUrl: "", apiKey: "secret", model: "m", configMode: "provider", agentIds: ["codex"] })).resolves.toBe(profile);

    expect(bridge.probe).toHaveBeenCalledWith({ provider: "custom", api_base_url: "https://proxy.test/v1", api_key: "secret", model: "m", agents: null });
    expect(bridge.getProvider).toHaveBeenCalledWith({ id: "acme" });
    expect(bridge.deleteProvider).toHaveBeenCalledWith({ id: "acme" });
    expect(bridge.install).toHaveBeenCalledWith(expect.objectContaining({ agents: ["codex"], profile_agents: null, timeout: 180, latest: false }));
    expect(bridge.register).toHaveBeenCalledWith({ provider: "ppio", agents: null });
    expect(bridge.activate).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "codex", profile_id: "", small_fast_model: "" }));
    expect(bridge.saveProfile).toHaveBeenCalledWith(expect.objectContaining({ api_base_url: "", api_key: "secret", agent_ids: ["codex"] }));
  });

  it("restores structured Wails errors without exposing raw bridge details", async () => {
    expect(normalizeWailsError({ cause: { error_code: "API_KEY_REJECTED", message: "key rejected", status: 401, retryable: false } })).toMatchObject({
      message: "key rejected", code: "API_KEY_REJECTED", status: 401, retryable: false,
    });
    expect(normalizeWailsError({ cause: '{"error_code":"TIMEOUT","message":"probe timed out","status":504,"retryable":true}' })).toMatchObject({
      message: "probe timed out", code: "TIMEOUT", status: 504, retryable: true,
    });
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
