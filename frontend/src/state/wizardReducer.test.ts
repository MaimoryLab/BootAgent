import { describe, expect, it } from "vitest";

import type { ModelsResponse, ProbeResponse, StatusResponse } from "../types/api";
import { initialWizardState, wizardReducer, type WizardState } from "./wizardReducer";

const status = {
  apiVersion: 1,
  platform: { os: "linux", arch: "x64", shell: "bash" },
  capabilities: { canInstall: {}, supportedAgentIds: [] },
  agents: {},
  catalog: [],
  groups: [],
  providers: {},
  paths: {},
  backups: {},
  environment: null,
  environmentError: null,
} satisfies StatusResponse;

const successProbe = {
  ok: true,
  reachable: true,
  status: 200,
  message: "ok",
  error_code: null,
  retryable: false,
} satisfies ProbeResponse;

describe("wizardReducer", () => {
  it("loads status and reports status errors", () => {
    let state = wizardReducer(initialWizardState, { type: "STATUS_LOADING" });
    expect(state.statusState).toBe("loading");
    state = wizardReducer(state, { type: "STATUS_LOADED", status });
    expect(state.status).toBe(status);
    state = wizardReducer(state, { type: "STATUS_FAILED", message: "offline" });
    expect(state.statusError).toBe("offline");
  });

  it("toggles agents without duplicates", () => {
    let state = wizardReducer(initialWizardState, { type: "TOGGLE_AGENT", agentId: "codex" });
    expect(state.selectedAgentIds).toEqual(["codex"]);
    state = wizardReducer(state, { type: "TOGGLE_AGENT", agentId: "opencode" });
    expect(state.selectedAgentIds).toEqual(["codex", "opencode"]);
    state = wizardReducer(state, { type: "TOGGLE_AGENT", agentId: "codex" });
    expect(state.selectedAgentIds).toEqual(["opencode"]);
  });

  it("keeps secrets out of state and resets dependent provider state", () => {
    let state: WizardState = {
      ...initialWizardState,
      connection: successProbe,
      connectionState: "success" as const,
      models: ["model-a"],
      modelsState: "success" as const,
      model: "model-a",
    };
    state = wizardReducer(state, { type: "SET_HAS_API_KEY", value: true });
    expect(state.hasApiKey).toBe(true);
    expect(JSON.stringify(state)).not.toContain("api_key");
    state = wizardReducer(state, { type: "SET_PROVIDER", value: "novita" });
    expect(state.connection).toBeNull();
    expect(state.models).toEqual([]);
    expect(state.model).toBe("");
    state = wizardReducer(state, { type: "SET_CUSTOM_BASE", value: "http://127.0.0.1:9000" });
    expect(state.customBaseUrl).toContain("127.0.0.1");
    state = wizardReducer(state, { type: "SET_HAS_API_KEY", value: false });
    expect(state.connectionState).toBe("idle");
  });

  it("marks skipped configuration steps and clears model selection", () => {
    const providerState = wizardReducer(initialWizardState, { type: "SET_CONFIG_MODE", value: "provider" });
    expect(providerState.configMode).toBe("provider");
    const existing = wizardReducer({ ...providerState, model: "model-a" }, { type: "SET_CONFIG_MODE", value: "existing-account" });
    expect(existing.configMode).toBe("existing-account");
    expect(existing.model).toBe("");
    expect(wizardReducer(existing, { type: "SET_INSTALL_MISSING", value: false }).installMissingAgents).toBe(false);
  });

  it("maps connection states", () => {
    let state = wizardReducer(initialWizardState, { type: "CONNECTION_LOADING" });
    expect(state.connectionState).toBe("loading");
    state = wizardReducer(state, { type: "CONNECTION_RESULT", result: successProbe });
    expect(state.connectionState).toBe("success");
    state = wizardReducer(state, {
      type: "CONNECTION_RESULT",
      result: { ...successProbe, ok: false, message: "rejected", error_code: "API_KEY_REJECTED" },
    });
    expect(state.connectionState).toBe("error");
    state = wizardReducer(state, { type: "CONNECTION_FAILED", message: "network" });
    expect(state.connection?.error_code).toBe("INTERNAL_ERROR");
  });

  it("uses the first discovered model and falls back to manual input", () => {
    const models = {
      ...successProbe,
      models: ["model-a", "model-b"],
      message: "found",
    } satisfies ModelsResponse;
    let state = wizardReducer(initialWizardState, { type: "MODELS_LOADING" });
    state = wizardReducer(state, { type: "MODELS_RESULT", result: models });
    expect(state.model).toBe("model-a");
    state = wizardReducer(state, { type: "SET_MODEL", value: "manual-model" });
    state = wizardReducer(state, { type: "MODELS_RESULT", result: models });
    expect(state.model).toBe("manual-model");
    state = wizardReducer({ ...initialWizardState, model: "" }, { type: "MODELS_FAILED", message: "unsupported" });
    expect(state.model).toBe("gpt-4.1");
  });

  it("merges retry results and preserves non-secret activation summary", () => {
    let state = wizardReducer(initialWizardState, { type: "ACTIVATION_LOADING", agentIds: ["codex", "opencode"] });
    expect(state.activationResults).toHaveLength(2);
    state = wizardReducer(state, {
      type: "ACTIVATION_RESULT",
      results: [
        { agent: "codex", status: "failed", message: "npm missing", retryable: true },
        { agent: "opencode", status: "configured", retryable: false },
      ],
      log: "first",
      probe: null,
      next: "opencode",
      replaceAgents: ["codex", "opencode"],
    });
    expect(state.activationState).toBe("error");
    state = wizardReducer(state, {
      type: "ACTIVATION_RESULT",
      results: [{ agent: "codex", status: "configured", retryable: false }],
      log: "retry",
      probe: successProbe,
      replaceAgents: ["codex"],
    });
    expect(state.activationState).toBe("success");
    expect(state.activationResults.map((item) => item.agent).sort()).toEqual(["codex", "opencode"]);
    expect(state.activationLog).toContain("first\n\nretry");
    expect(state.activationNext).toBe("opencode");
    expect(state.activationProbe?.ok).toBe(true);
  });

  it("handles activation transport failure and setup reset", () => {
    let state = wizardReducer({ ...initialWizardState, selectedAgentIds: ["codex"], status }, { type: "ACTIVATION_FAILED", message: "failed" });
    expect(state.activationState).toBe("error");
    state = wizardReducer(state, { type: "RESET_SETUP" });
    expect(state.selectedAgentIds).toEqual([]);
    expect(state.status).toBe(status);
  });
});
