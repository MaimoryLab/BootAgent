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
  mirrors: [],
  paths: {},
  backups: {},
  environment: null,
  environmentError: null,
  profiles: [],
  activeProfile: null,
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

  it("invalidates a stale probe verdict whenever the key changes", () => {
    // The reducer only sees the non-empty boolean (the secret stays in a ref),
    // so editing "valid key" into "wrong key" dispatches the very same
    // SET_HAS_API_KEY(true). It must reset the verdict: keeping the stale
    // success would let a wrong key through the provider gate.
    const probed: WizardState = {
      ...initialWizardState,
      hasApiKey: true,
      connection: successProbe,
      connectionState: "success" as const,
    };
    const edited = wizardReducer(probed, { type: "SET_HAS_API_KEY", value: true });
    expect(edited.hasApiKey).toBe(true);
    expect(edited.connectionState).toBe("idle");
    expect(edited.connection).toBeNull();
    const cleared = wizardReducer(probed, { type: "SET_HAS_API_KEY", value: false });
    expect(cleared.connectionState).toBe("idle");
    expect(cleared.connection).toBeNull();
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
    // A transport failure must carry the thrown error's contract through,
    // not overwrite it with hard-coded placeholders.
    state = wizardReducer(state, {
      type: "CONNECTION_FAILED",
      failure: { message: "key rejected", code: "API_KEY_REJECTED", retryable: false },
    });
    expect(state.connection).toMatchObject({ message: "key rejected", error_code: "API_KEY_REJECTED", retryable: false });
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
    // Discovery failure must NOT inject a phantom default: the field stays
    // empty so the page forces an explicit, user-confirmed model ID.
    state = wizardReducer({ ...initialWizardState, model: "" }, { type: "MODELS_FAILED", message: "unsupported" });
    expect(state.model).toBe("");
    expect(state.modelsMessage).toBe("unsupported");
    state = wizardReducer(
      { ...initialWizardState, model: "" },
      { type: "MODELS_RESULT", result: { ...models, ok: false, models: [] } },
    );
    expect(state.model).toBe("");
  });

  it("arms activation only through an explicit request and disarms it on start", () => {
    // Remounting the activation page (browser back) must find the flag down,
    // otherwise the install side effect would replay with a cleared key.
    let state = wizardReducer(initialWizardState, { type: "REQUEST_ACTIVATION" });
    expect(state.activationRequested).toBe(true);
    state = wizardReducer(state, { type: "ACTIVATION_LOADING", agentIds: ["codex"] });
    expect(state.activationRequested).toBe(false);
    expect(state.activationState).toBe("loading");
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
    // The failure message appends to the log instead of erasing what already ran.
    let state = wizardReducer(
      { ...initialWizardState, selectedAgentIds: ["codex"], status, activationLog: "install log" },
      { type: "ACTIVATION_FAILED", message: "failed" },
    );
    expect(state.activationState).toBe("error");
    expect(state.activationLog).toBe("install log\n\nfailed");
    state = wizardReducer(state, { type: "RESET_SETUP" });
    expect(state.selectedAgentIds).toEqual([]);
    expect(state.status).toBe(status);
  });

  it("returns the same state for an unknown action", () => {
    // Guards against a dispatch typo silently resetting the wizard.
    const before: WizardState = { ...initialWizardState, selectedAgentIds: ["codex"], model: "m" };
    const after = wizardReducer(before, { type: "NOT_A_REAL_ACTION" } as never);
    expect(after).toBe(before);
  });
});
