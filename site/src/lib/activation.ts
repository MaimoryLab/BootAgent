import { compatibilityFor, type Compatibility, type SiteCatalogV2 } from "./explorer";

type ActivationPhase =
  | "idle"
  | "scanning"
  | "agent"
  | "mode"
  | "provider"
  | "model"
  | "verifying"
  | "ready"
  | "guide-only"
  | "preview-gate"
  | "unsupported"
  | "error";

/* Mirrors the product's two ConfigModePage choices. "existing-account" is the
   real branch for a user who already has a subscription or a working config —
   it skips the provider and model steps, exactly as SetupGuard does. */
export type ConfigMode = "provider" | "existing-account";

export const CUSTOM_PROVIDER_ID = "custom";

export interface ActivationState {
  phase: ActivationPhase;
  agentId: string | null;
  configMode: ConfigMode | null;
  providerId: string | null;
  customBaseUrl: string;
  model: string | null;
  compatibility: Compatibility | null;
}

export type ActivationAction =
  | { type: "start" }
  | { type: "scan-complete" }
  | { type: "select-agent"; agentId: string }
  | { type: "select-mode"; mode: ConfigMode }
  | { type: "select-provider"; providerId: string }
  | { type: "set-custom-base-url"; value: string }
  | { type: "select-model"; model: string }
  | { type: "verify" }
  | { type: "verify-complete" }
  | { type: "confirm" }
  | { type: "fail" }
  | { type: "reset" };

export function initialActivationState(): ActivationState {
  return {
    phase: "idle",
    agentId: null,
    configMode: null,
    providerId: null,
    customBaseUrl: "",
    model: null,
    compatibility: null,
  };
}

type BaseUrlRejection =
  | "required"
  | "control-characters"
  | "scheme"
  | "credentials";

export type BaseUrlCheck =
  | { ok: true; value: string }
  | { ok: false; reason: BaseUrlRejection };

/* A line-for-line port of validate_base_url in oneagent/providers.py, in the
   same order, so the demo rejects exactly what the kernel would reject. The
   demo is only worth showing if this agrees with the product; a looser check
   here would teach visitors an endpoint is acceptable when it is not. */
export function validateDemoBaseUrl(value: string): BaseUrlCheck {
  if (!value) return { ok: false, reason: "required" };
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0;
    if (code < 32 || code === 127) return { ok: false, reason: "control-characters" };
  }
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return { ok: false, reason: "scheme" };
  }
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || !parsed.host) {
    return { ok: false, reason: "scheme" };
  }
  if (parsed.username || parsed.password) return { ok: false, reason: "credentials" };
  return { ok: true, value: value.replace(/\/+$/, "") };
}

function errorState(state: ActivationState): ActivationState {
  return { ...state, phase: "error" };
}

export function activationReducer(
  state: ActivationState,
  action: ActivationAction,
  catalog: SiteCatalogV2,
): ActivationState {
  if (action.type === "reset") return initialActivationState();

  switch (action.type) {
    case "start":
      return state.phase === "idle" ? { ...initialActivationState(), phase: "scanning" } : state;
    case "scan-complete":
      return state.phase === "scanning" ? { ...state, phase: "agent" } : state;
    case "select-agent": {
      const agent = catalog.agents.find((candidate) => candidate.id === action.agentId);
      if (!agent) return errorState(state);
      const base = { ...initialActivationState(), agentId: agent.id };
      if (agent.support.officialInstallGuide || !agent.support.managedConfig || !agent.protocol) {
        return { ...base, phase: "guide-only" };
      }
      return { ...base, phase: "mode" };
    }
    case "select-mode": {
      if (state.phase !== "mode" || !state.agentId) return state;
      /* Picking an existing account ends the demo at the same place the product
         lands: nothing to verify, because no new credential is being introduced. */
      if (action.mode === "existing-account") {
        return { ...state, phase: "ready", configMode: action.mode, providerId: null, model: null, compatibility: null };
      }
      return { ...state, phase: "provider", configMode: action.mode };
    }
    case "select-provider": {
      const agent = catalog.agents.find((candidate) => candidate.id === state.agentId);
      if (!agent) return errorState(state);
      /* A custom endpoint has no catalog entry, so there is no published
         protocol status to read. The product treats it as usable-but-unproven:
         the probe decides, which here means the demo's own verify step. */
      if (action.providerId === CUSTOM_PROVIDER_ID) {
        return { ...state, phase: "provider", providerId: CUSTOM_PROVIDER_ID, compatibility: "supported" };
      }
      const provider = catalog.providers.find((candidate) => candidate.id === action.providerId);
      if (!provider) return errorState(state);
      const compatibility = compatibilityFor(agent, provider);
      if (compatibility === "unsupported") {
        return { ...state, phase: "unsupported", providerId: provider.id, customBaseUrl: "", compatibility };
      }
      return { ...state, phase: "provider", providerId: provider.id, customBaseUrl: "", compatibility };
    }
    case "set-custom-base-url":
      return state.providerId === CUSTOM_PROVIDER_ID ? { ...state, customBaseUrl: action.value } : state;
    case "select-model":
      return state.phase === "model" && action.model ? { ...state, model: action.model } : state;
    case "verify": {
      if (state.phase !== "provider" || !state.agentId || !state.providerId || !state.compatibility) {
        return errorState(state);
      }
      // An unvalidated endpoint must not reach verification, the same way the
      // product's probe button stays disabled until a custom base URL parses.
      if (state.providerId === CUSTOM_PROVIDER_ID && !validateDemoBaseUrl(state.customBaseUrl).ok) {
        return state;
      }
      return { ...state, phase: "verifying" };
    }
    case "verify-complete":
      if (state.phase !== "verifying") return state;
      if (state.compatibility === "preview-gate") return { ...state, phase: "preview-gate" };
      if (state.compatibility === "supported" || state.compatibility === "verified") {
        // Verification unlocks model choice; it is not the end of the flow.
        return { ...state, phase: "model" };
      }
      return { ...state, phase: "unsupported" };
    case "confirm":
      return state.phase === "model" && state.model ? { ...state, phase: "ready" } : state;
    case "fail":
      return errorState(state);
  }
}
