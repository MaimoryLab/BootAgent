import type { FailureDetail } from "../backend/errors";
import type {
  AgentInstallResult,
  ModelsResponse,
  ProbeResponse,
  ProviderId,
  StatusResponse,
} from "../types/api";

export type ConfigMode = "provider" | "existing-account" | null;
export type AsyncState = "idle" | "loading" | "success" | "error";

export interface WizardState {
  status: StatusResponse | null;
  statusState: AsyncState;
  statusError: string;
  selectedAgentIds: string[];
  installMissingAgents: boolean;
  configMode: ConfigMode;
  provider: ProviderId;
  customBaseUrl: string;
  hasApiKey: boolean;
  connection: ProbeResponse | null;
  connectionState: AsyncState;
  models: string[];
  modelsState: AsyncState;
  modelsMessage: string;
  model: string;
  /** Set by the review page's explicit "start" action; consumed by the
   *  activation page so remounting it (browser back) never replays installs. */
  activationRequested: boolean;
  activationState: AsyncState;
  activationResults: AgentInstallResult[];
  activationLog: string;
  activationProbe: ProbeResponse | null;
  activationNext: string;
}

export const initialWizardState: WizardState = {
  status: null,
  statusState: "idle",
  statusError: "",
  selectedAgentIds: [],
  installMissingAgents: true,
  configMode: null,
  provider: "ppio",
  customBaseUrl: "",
  hasApiKey: false,
  connection: null,
  connectionState: "idle",
  models: [],
  modelsState: "idle",
  modelsMessage: "",
  model: "",
  activationRequested: false,
  activationState: "idle",
  activationResults: [],
  activationLog: "",
  activationProbe: null,
  activationNext: "",
};

export type WizardAction =
  | { type: "STATUS_LOADING" }
  | { type: "STATUS_LOADED"; status: StatusResponse }
  | { type: "STATUS_FAILED"; message: string }
  | { type: "TOGGLE_AGENT"; agentId: string }
  | { type: "SET_INSTALL_MISSING"; value: boolean }
  | { type: "SET_CONFIG_MODE"; value: Exclude<ConfigMode, null> }
  | { type: "SET_PROVIDER"; value: ProviderId }
  | { type: "SET_CUSTOM_BASE"; value: string }
  | { type: "SET_HAS_API_KEY"; value: boolean }
  | { type: "CONNECTION_LOADING" }
  | { type: "CONNECTION_RESULT"; result: ProbeResponse }
  | { type: "CONNECTION_FAILED"; failure: FailureDetail }
  | { type: "MODELS_LOADING" }
  | { type: "MODELS_RESULT"; result: ModelsResponse }
  | { type: "MODELS_FAILED"; message: string }
  | { type: "SET_MODEL"; value: string }
  | { type: "REQUEST_ACTIVATION" }
  | { type: "ACTIVATION_LOADING"; agentIds: string[] }
  | {
      type: "ACTIVATION_RESULT";
      results: AgentInstallResult[];
      log: string;
      probe: ProbeResponse | null;
      next?: string;
      replaceAgents?: string[];
    }
  | { type: "ACTIVATION_FAILED"; message: string }
  | { type: "RESET_SETUP" };

function mergeResults(
  current: AgentInstallResult[],
  incoming: AgentInstallResult[],
  replaceAgents: string[] = [],
): AgentInstallResult[] {
  const replacements = new Set(replaceAgents);
  const incomingIds = new Set(incoming.map((item) => item.agent));
  return [
    ...current.filter((item) => !replacements.has(item.agent) && !incomingIds.has(item.agent)),
    ...incoming,
  ];
}

export function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case "STATUS_LOADING":
      return { ...state, statusState: "loading", statusError: "" };
    case "STATUS_LOADED":
      return { ...state, status: action.status, statusState: "success", statusError: "" };
    case "STATUS_FAILED":
      return { ...state, statusState: "error", statusError: action.message };
    case "TOGGLE_AGENT":
      return {
        ...state,
        selectedAgentIds: state.selectedAgentIds.includes(action.agentId)
          ? state.selectedAgentIds.filter((id) => id !== action.agentId)
          : [...state.selectedAgentIds, action.agentId],
      };
    case "SET_INSTALL_MISSING":
      return { ...state, installMissingAgents: action.value };
    case "SET_CONFIG_MODE":
      return {
        ...state,
        configMode: action.value,
        connection: null,
        connectionState: "idle",
        models: [],
        modelsState: "idle",
        modelsMessage: "",
        model: action.value === "existing-account" ? "" : state.model,
      };
    case "SET_PROVIDER":
      return {
        ...state,
        provider: action.value,
        connection: null,
        connectionState: "idle",
        models: [],
        modelsState: "idle",
        modelsMessage: "",
        model: "",
      };
    case "SET_CUSTOM_BASE":
      return {
        ...state,
        customBaseUrl: action.value,
        connection: null,
        connectionState: "idle",
        models: [],
        modelsState: "idle",
      };
    case "SET_HAS_API_KEY":
      // Any edit of the key invalidates the previous probe verdict. The reducer
      // only sees the non-empty boolean (the secret itself stays in a ref), so
      // it cannot tell "same key" from "different key" and must assume a change:
      // keeping a stale success would let a wrong key through the provider gate.
      return {
        ...state,
        hasApiKey: action.value,
        connection: null,
        connectionState: "idle",
      };
    case "CONNECTION_LOADING":
      return { ...state, connectionState: "loading", connection: null };
    case "CONNECTION_RESULT":
      return {
        ...state,
        connection: action.result,
        connectionState: action.result.ok ? "success" : "error",
      };
    case "CONNECTION_FAILED":
      return {
        ...state,
        connection: {
          ok: false,
          reachable: false,
          status: 0,
          message: action.failure.message,
          error_code: action.failure.code,
          retryable: action.failure.retryable,
        },
        connectionState: "error",
      };
    case "MODELS_LOADING":
      return { ...state, modelsState: "loading", modelsMessage: "" };
    case "MODELS_RESULT":
      return {
        ...state,
        models: action.result.models,
        modelsState: action.result.ok ? "success" : "error",
        modelsMessage: action.result.message,
        // No placeholder fallback: when discovery finds nothing the field stays
        // empty and the page requires a manual, user-confirmed model ID.
        model: state.model || action.result.models[0] || "",
      };
    case "MODELS_FAILED":
      return {
        ...state,
        models: [],
        modelsState: "error",
        modelsMessage: action.message,
      };
    case "SET_MODEL":
      return { ...state, model: action.value };
    case "REQUEST_ACTIVATION":
      return { ...state, activationRequested: true };
    case "ACTIVATION_LOADING":
      return {
        ...state,
        activationRequested: false,
        activationState: "loading",
        activationResults: state.activationResults.length
          ? state.activationResults
          : action.agentIds.map((agent) => ({ agent, status: "skipped", retryable: false })),
      };
    case "ACTIVATION_RESULT": {
      const activationResults = mergeResults(state.activationResults, action.results, action.replaceAgents);
      return {
        ...state,
        activationState: activationResults.some((item) => item.status === "failed") ? "error" : "success",
        activationResults,
        activationLog: [state.activationLog, action.log].filter(Boolean).join("\n\n"),
        activationProbe: action.probe ?? state.activationProbe,
        activationNext: action.next ?? state.activationNext,
      };
    }
    case "ACTIVATION_FAILED":
      return {
        ...state,
        activationState: "error",
        // Append like ACTIVATION_RESULT does: a transport failure on retry must
        // not erase the logs of the installs that already ran.
        activationLog: [state.activationLog, action.message].filter(Boolean).join("\n\n"),
      };
    case "RESET_SETUP":
      return {
        ...initialWizardState,
        status: state.status,
        statusState: state.statusState,
        statusError: state.statusError,
      };
    default:
      return state;
  }
}
