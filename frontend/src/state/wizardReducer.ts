import type { FailureDetail } from "../backend/errors";
import type {
  AgentInstallResult,
  ModelsResponse,
  ProbeResponse,
  ProviderId,
  StatusResponse,
  InstallOutput,
} from "../types/api";

export type AsyncState = "idle" | "loading" | "success" | "error";

export interface WizardState {
  status: StatusResponse | null;
  statusState: AsyncState;
  statusError: string;
  /** Onboarding installs exactly one Agent; the array shape stays because the
   *  install API and the activation page are both multi-Agent. */
  selectedAgentIds: string[];
  installMissingAgents: boolean;
  provider: ProviderId;
  /** Profile id/label written by the confirm step. Empty means "derive from
   *  Agent and Provider". */
  profileId: string;
  profileLabel: string;
  hasApiKey: boolean;
  connection: ProbeResponse | null;
  connectionState: AsyncState;
  /** A probe with this key succeeded. Separate from connectionState because the
   *  model steps sits *after* the provider gate: choosing a model re-opens the
   *  verdict for display, but it does not make the key unverified, and treating
   *  it that way sent the user backwards out of the model step. Cleared only
   *  when the key or the Provider changes. */
  keyVerified: boolean;
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
  provider: "ppio",
  profileId: "",
  profileLabel: "",
  hasApiKey: false,
  connection: null,
  connectionState: "idle",
  keyVerified: false,
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
  | { type: "SELECT_AGENT"; agentId: string }
  | { type: "SET_INSTALL_MISSING"; value: boolean }
  | { type: "SET_PROVIDER"; value: ProviderId }
  | { type: "SET_PROFILE_ID"; value: string }
  | { type: "SET_PROFILE_LABEL"; value: string }
  | { type: "START_SETUP"; profileId?: string; profileLabel?: string }
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
  | { type: "ACTIVATION_OUTPUT"; output: InstallOutput }
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

function formatCommand(args: string[]): string {
  return args.map((arg) => (/^[A-Za-z0-9_./:@%+=,-]+$/.test(arg) ? arg : JSON.stringify(arg))).join(" ");
}

function appendActivationOutput(log: string, output: InstallOutput): string {
  // Download progress is a bar, not a log line: the Task Center and the install
  // prompts render it, and appending byte counts here would bury the commands.
  if (output.kind === "progress") return log;
  const text = output.kind === "command" ? `$ ${formatCommand(output.args)}\n` : output.text;
  if (!text) return log;
  return `${log}${output.kind === "command" && log && !log.endsWith("\n") ? "\n" : ""}${text}`;
}

export function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.type) {
    case "STATUS_LOADING":
      return { ...state, statusState: "loading", statusError: "" };
    case "STATUS_LOADED":
      return { ...state, status: action.status, statusState: "success", statusError: "" };
    case "STATUS_FAILED":
      return { ...state, statusState: "error", statusError: action.message };
    case "SELECT_AGENT":
      // Single select, and re-clicking the current row keeps it selected: the
      // step cannot continue with nothing chosen, so a toggle-off would only
      // ever produce a dead end.
      return { ...state, selectedAgentIds: [action.agentId] };
    case "SET_INSTALL_MISSING":
      return { ...state, installMissingAgents: action.value };
    case "SET_PROFILE_ID":
      return { ...state, profileId: action.value };
    case "SET_PROFILE_LABEL":
      return { ...state, profileLabel: action.value };
    case "START_SETUP":
      // Entering onboarding from the overview or the Profile page must not
      // inherit a previous run's Agent, model or install log.
      return {
        ...initialWizardState,
        status: state.status,
        statusState: state.statusState,
        statusError: state.statusError,
        profileId: action.profileId ?? "",
        profileLabel: action.profileLabel ?? "",
      };
    case "SET_PROVIDER":
      return {
        ...state,
        provider: action.value,
        connection: null,
        connectionState: "idle",
        keyVerified: false,
        models: [],
        modelsState: "idle",
        modelsMessage: "",
        model: "",
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
        keyVerified: false,
      };
    case "CONNECTION_LOADING":
      return { ...state, connectionState: "loading", connection: null };
    case "CONNECTION_RESULT":
      return {
        ...state,
        connection: action.result,
        connectionState: action.result.ok ? "success" : "error",
        keyVerified: state.keyVerified || action.result.ok,
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
      return { ...state, model: action.value, connection: null, connectionState: "idle" };
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
    case "ACTIVATION_OUTPUT":
      return { ...state, activationLog: appendActivationOutput(state.activationLog, action.output) };
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
