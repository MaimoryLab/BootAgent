import { describe, expect, it } from "vitest";

import type { ModelsResponse, ProbeResponse, StatusResponse } from "../types/api";
import { initialWizardState, wizardNeedsModel, wizardReducer, type WizardState } from "./wizardReducer";

const status = {
  apiVersion: 1,
  platform: { os: "linux", arch: "x64", shell: "bash" },
  runtimes: [],
  capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] },
  agents: {},
  catalog: [],
  groups: [],
  providers: {},
  mirrors: [],
  paths: {},
  backups: {},
  environment: null,
  environmentError: null,
  desktopAgents: [],
  profiles: [],
  activeProfile: null,
  firstRun: false,
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
  it("defaults Agent selection to desktop", () => {
    expect(initialWizardState.setupKind).toBe("desktop");
  });

  it("loads status and reports status errors", () => {
    let state = wizardReducer(initialWizardState, { type: "STATUS_LOADING" });
    expect(state.statusState).toBe("loading");
    state = wizardReducer(state, { type: "STATUS_LOADED", status });
    expect(state.status).toBe(status);
    state = wizardReducer(state, { type: "STATUS_FAILED", message: "offline" });
    expect(state.statusError).toBe("offline");
  });

  it("defaults setup to the newest Provider", () => {
    const providers = {
      ppio: { name: "PPIO", home: "", base_url: "", custom: false, created_at: "" },
      newer: { name: "Newer", home: "", base_url: "", custom: true, created_at: "2026-02-01T00:00:00Z" },
    } satisfies StatusResponse["providers"];
    const loaded = wizardReducer(initialWizardState, { type: "STATUS_LOADED", status: { ...status, providers } });
    expect(loaded.provider).toBe("newer");
    expect(wizardReducer(loaded, { type: "START_SETUP" }).provider).toBe("newer");
  });

  it("selects exactly one Agent and cannot be emptied by re-selecting", () => {
    // Onboarding installs one Agent per run, and every step after this one
    // requires a selection: a toggle-off would only produce a dead end.
    let state = wizardReducer(initialWizardState, { type: "SELECT_AGENT", agentId: "codex" });
    expect(state.selectedAgentIds).toEqual(["codex"]);
    state = wizardReducer(state, { type: "SELECT_AGENT", agentId: "opencode" });
    expect(state.selectedAgentIds).toEqual(["opencode"]);
    state = wizardReducer(state, { type: "SELECT_AGENT", agentId: "opencode" });
    expect(state.selectedAgentIds).toEqual(["opencode"]);
  });

  it("does not carry a derived Profile into a different Agent pairing", () => {
    const state = wizardReducer(
      {
        ...initialWizardState,
        selectedAgentIds: ["codex"],
        profileId: "codex-ppio",
        profileLabel: "Team",
        connection: successProbe,
        connectionState: "success",
        keyVerified: true,
        models: ["old-model"],
        modelsState: "success",
        modelsMessage: "old list",
        model: "old-model",
      },
      { type: "SELECT_AGENT", agentId: "opencode" },
    );
    expect(state.profileId).toBe("");
    expect(state.profileLabel).toBe("");
    expect(state.connection).toBeNull();
    expect(state.connectionState).toBe("idle");
    expect(state.keyVerified).toBe(false);
    expect(state.models).toEqual([]);
    expect(state.modelsState).toBe("idle");
    expect(state.modelsMessage).toBe("");
    expect(state.model).toBe("");
  });

  it("clears a previous run when setup restarts but keeps the status snapshot", () => {
    // Entering onboarding again from the overview or the Profile page must not
    // inherit the last run's Agent, model or install log.
    const used: WizardState = {
      ...initialWizardState,
      status: status,
      statusState: "success",
      selectedAgentIds: ["codex"],
      model: "model-a",
      activationLog: "old log",
      activationState: "success",
    };
    const restarted = wizardReducer(used, { type: "START_SETUP", profileLabel: "Team PPIO" });
    expect(restarted.selectedAgentIds).toEqual([]);
    expect(restarted.model).toBe("");
    expect(restarted.activationLog).toBe("");
    expect(restarted.activationState).toBe("idle");
    expect(restarted.status).toBe(status);
    expect(restarted.profileLabel).toBe("Team PPIO");
  });

  it("keeps secrets out of state and resets dependent provider state", () => {
    let state: WizardState = {
      ...initialWizardState,
      connection: successProbe,
      connectionState: "success" as const,
      models: ["model-a"],
      modelsState: "success" as const,
      model: "model-a",
      profileId: "codex-ppio",
      profileLabel: "Team PPIO",
    };
    state = wizardReducer(state, { type: "SET_HAS_API_KEY", value: true });
    expect(state.hasApiKey).toBe(true);
    expect(JSON.stringify(state)).not.toContain("api_key");
    state = wizardReducer(state, { type: "SET_PROVIDER", value: "novita" });
    expect(state.connection).toBeNull();
    expect(state.models).toEqual([]);
    expect(state.model).toBe("");
    expect(state.profileId).toBe("");
    expect(state.profileLabel).toBe("");
    state = wizardReducer(state, { type: "SET_HAS_API_KEY", value: false });
    expect(state.connectionState).toBe("idle");
  });

  it("invalidates a stale probe verdict whenever the key or model changes", () => {
    // The reducer only sees the non-empty boolean (the secret stays in a ref),
    // so editing "valid key" into "wrong key" dispatches the very same
    // SET_HAS_API_KEY(true). It must reset the verdict: keeping the stale
    // success would let a wrong key through the provider gate.
    const probed: WizardState = {
      ...initialWizardState,
      hasApiKey: true,
      connection: successProbe,
      connectionState: "success" as const,
      keyVerified: true,
    };
    const edited = wizardReducer(probed, { type: "SET_HAS_API_KEY", value: true });
    expect(edited.hasApiKey).toBe(true);
    expect(edited.connectionState).toBe("idle");
    expect(edited.connection).toBeNull();
    expect(edited.keyVerified).toBe(false);
    const cleared = wizardReducer(probed, { type: "SET_HAS_API_KEY", value: false });
    expect(cleared.connectionState).toBe("idle");
    expect(cleared.connection).toBeNull();
    expect(cleared.keyVerified).toBe(false);
    const changedModel = wizardReducer(probed, { type: "SET_MODEL", value: "vendor-model" });
    expect(changedModel.connectionState).toBe("idle");
    expect(changedModel.connection).toBeNull();
    // ...but the key itself is still known-good. The model step comes after the
    // provider gate, so treating a model pick as "key unverified" bounced the
    // user out of the step they had just reached.
    expect(changedModel.keyVerified).toBe(true);
    // Switching Provider does invalidate it: the same key proves nothing
    // about a different endpoint.
    expect(wizardReducer(probed, { type: "SET_PROVIDER", value: "novita" }).keyVerified).toBe(false);
  });

  it("records the profile name", () => {
    expect(wizardReducer(initialWizardState, { type: "SET_PROFILE_LABEL", value: "Team PPIO" }).profileLabel).toBe("Team PPIO");
    expect(wizardReducer(initialWizardState, { type: "SET_PROFILE_ID", value: "codex-ppio" }).profileId).toBe("codex-ppio");
  });

  it("loads an existing profile without reopening model discovery", () => {
    const selected = wizardReducer(initialWizardState, {
      type: "SELECT_PROFILE",
      provider: "ppio",
      profileId: "team",
      profileLabel: "Team",
      model: "model-a",
      keyVerified: true,
    });
    expect(selected.profileId).toBe("team");
    expect(selected.profileLabel).toBe("Team");
    expect(selected.model).toBe("model-a");
    expect(selected.keyVerified).toBe(true);
    expect(selected.connectionState).toBe("success");
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

  it("reuses the custom probe model on the model step", () => {
    const models = { ...successProbe, models: ["listed-model"], message: "found" } satisfies ModelsResponse;
    let state = wizardReducer({ ...initialWizardState, probeModel: "vendor/test-model" }, { type: "MODELS_RESULT", result: models });
    expect(state.model).toBe("vendor/test-model");
    state = wizardReducer({ ...initialWizardState, probeModel: "vendor/test-model" }, { type: "MODELS_FAILED", message: "unsupported" });
    expect(state.model).toBe("vendor/test-model");
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

  it("appends installation commands and output as it arrives", () => {
    let state = wizardReducer(initialWizardState, { type: "ACTIVATION_LOADING", agentIds: ["codex"] });
    state = wizardReducer(state, { type: "ACTIVATION_OUTPUT", output: { kind: "command", args: ["npm", "install", "-g", "agent@1.0.0"] } });
    state = wizardReducer(state, { type: "ACTIVATION_OUTPUT", output: { kind: "output", stream: "stdout", text: "fetching\n" } });
    state = wizardReducer(state, { type: "ACTIVATION_OUTPUT", output: { kind: "output", stream: "stderr", text: "warning\n" } });
    expect(state.activationLog).toBe("$ npm install -g agent@1.0.0\nfetching\nwarning\n");
  });

  it("merges retry results and preserves non-secret activation summary", () => {
    let state = wizardReducer(initialWizardState, { type: "ACTIVATION_LOADING", agentIds: ["codex", "opencode"] });
    expect(state.activationResults).toHaveLength(2);
    state = wizardReducer(state, {
      type: "ACTIVATION_RESULT",
      ok: false,
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
      ok: true,
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

  it("keeps an aggregate install failure out of the success state", () => {
    const state = wizardReducer(initialWizardState, {
      type: "ACTIVATION_RESULT",
      ok: false,
      results: [{ agent: "codex", status: "configured", retryable: false }],
      log: "profile write failed",
      probe: null,
    });
    expect(state.activationState).toBe("error");
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

describe("provider default model", () => {
  // Two Providers with different defaults, plus one with none, which is what a
  // user-added Provider looks like.
  const withDefaults = {
    ...status,
    providers: {
      ppio: { name: "PPIO", home: "https://ppio.com/", base_url: "https://api.ppio.com/openai", default_model: "ppio/model-a" },
      novita: { name: "Novita", home: "https://novita.ai/", base_url: "https://api.novita.ai/openai", default_model: "novita/model-b" },
      acme: { name: "Acme", home: "https://acme.test/", base_url: "https://api.acme.test/openai", custom: true },
    },
  } satisfies StatusResponse;

  it("seeds the model from the Provider and re-seeds when the Provider changes", () => {
    let state = wizardReducer({ ...initialWizardState, status: withDefaults, provider: "ppio" }, { type: "SET_PROVIDER", value: "ppio" });
    expect(state.model).toBe("ppio/model-a");
    state = wizardReducer(state, { type: "SET_PROVIDER", value: "novita" });
    // Not carried over: a model ID from one Provider is rarely valid at another.
    expect(state.model).toBe("novita/model-b");
  });

  it("leaves the model empty for a Provider that publishes no default", () => {
    // A custom endpoint gets no guess, so the field stays required as before.
    const state = wizardReducer({ ...initialWizardState, status: withDefaults }, { type: "SET_PROVIDER", value: "acme" });
    expect(state.model).toBe("");
  });

  it("keeps a probe model the user typed instead of the seeded default", () => {
    const seeded = wizardReducer({ ...initialWizardState, status: withDefaults }, { type: "SET_PROVIDER", value: "ppio" });
    const typed = wizardReducer(seeded, { type: "SET_PROBE_MODEL", value: "ppio/hand-picked" });
    const state = wizardReducer(typed, {
      type: "MODELS_RESULT",
      result: { ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: ["ppio/discovered"] } satisfies ModelsResponse,
    });
    expect(state.model).toBe("ppio/hand-picked");
  });

  it("keeps a model chosen on the model step when discovery finishes later", () => {
    // The seeded default must not win over an explicit pick, which is the trap
    // in resolving this with a plain `state.model ||` now that it is never empty.
    const seeded = wizardReducer({ ...initialWizardState, status: withDefaults }, { type: "SET_PROVIDER", value: "ppio" });
    const chosen = wizardReducer(seeded, { type: "SET_MODEL", value: "ppio/chosen" });
    const state = wizardReducer(chosen, {
      type: "MODELS_RESULT",
      result: { ok: true, reachable: true, status: 200, message: "", error_code: null, retryable: false, models: ["ppio/discovered"] } satisfies ModelsResponse,
    });
    expect(state.model).toBe("ppio/chosen");
  });

  it("seeds on first status load and on starting a new run", () => {
    // byProviderCreatedAt puts user-added Providers ahead of built-ins, so the
    // initially selected Provider here is the one without a default. The point
    // is that the seed follows whichever Provider was picked, not that it is
    // always non-empty.
    const builtInOnly = { ...withDefaults, providers: { ppio: withDefaults.providers.ppio, novita: withDefaults.providers.novita } } satisfies StatusResponse;
    const loaded = wizardReducer(initialWizardState, { type: "STATUS_LOADED", status: builtInOnly });
    expect(loaded.model).toBe(builtInOnly.providers[loaded.provider as "ppio" | "novita"].default_model);
    // Starting over resets to the default rather than to an empty field.
    const restarted = wizardReducer({ ...loaded, model: "leftover" }, { type: "START_SETUP" });
    expect(restarted.model).toBe(loaded.model);
    const fresh = wizardReducer({ ...loaded, model: "leftover" }, { type: "START_NEW_PROFILE" });
    expect(fresh.model).toBe(loaded.model);
  });

  it("does not overwrite a model the user is editing when status refreshes", () => {
    // A refresh re-enters STATUS_LOADED; seeding there must be first-load only.
    const loaded = wizardReducer(initialWizardState, { type: "STATUS_LOADED", status: withDefaults });
    const editing = wizardReducer({ ...loaded, model: "half-typed" }, { type: "STATUS_LOADED", status: withDefaults });
    expect(editing.model).toBe("half-typed");
  });
});

describe("wizardNeedsModel", () => {
  const entry = (id: string, selectsModel: boolean) => ({
    id,
    name: id,
    group: "auto" as const,
    configMode: "auto" as const,
    selectsModel,
    webApp: false,
    guideOnly: false,
    lockedVersion: null,
    protocol: "openai" as const,
    platforms: ["macos" as const],
    platformNote: "",
    rank: 1,
  });
  const withCatalog = (selected: string[], catalog: ReturnType<typeof entry>[]): WizardState => ({
    ...initialWizardState,
    selectedAgentIds: selected,
    status: { ...status, catalog },
  });

  it("skips the model step when the Agent owns its own model choice", () => {
    expect(wizardNeedsModel(withCatalog(["dsh"], [entry("dsh", false)]))).toBe(false);
  });

  it("asks for a model for an ordinary Agent", () => {
    expect(wizardNeedsModel(withCatalog(["codex"], [entry("codex", true)]))).toBe(true);
  });

  // A mixed selection still asks: the Agents that can take a model should get
  // one, and skipping would silently leave them on whatever they had.
  it("asks when any selected Agent takes a model", () => {
    const state = withCatalog(["dsh", "codex"], [entry("dsh", false), entry("codex", true)]);
    expect(wizardNeedsModel(state)).toBe(true);
  });

  // Defaulting to true is what keeps a missing catalog from silently skipping a
  // step the Agent actually needs.
  it("asks when the status or the selection is not known yet", () => {
    expect(wizardNeedsModel(initialWizardState)).toBe(true);
    expect(wizardNeedsModel({ ...initialWizardState, selectedAgentIds: ["codex"] })).toBe(true);
    expect(wizardNeedsModel(withCatalog(["absent"], [entry("dsh", false)]))).toBe(true);
  });
});
