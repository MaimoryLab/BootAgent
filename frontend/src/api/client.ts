import type {
  ActivateAgentResponse,
  InstallRequest,
  InstallResponse,
  ModelsResponse,
  ProbeResponse,
  ProviderId,
  StatusResponse,
} from "../types/api";
import { OneAgentApiError } from "../backend/errors";

export { OneAgentApiError, describeError } from "../backend/errors";
export type { FailureDetail } from "../backend/errors";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, {
      ...init,
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...init?.headers,
      },
    });
  } catch {
    // fetch rejects with an opaque English TypeError on network failure; the
    // usual cause for this loopback-only app is the GUI process having exited.
    throw new OneAgentApiError("无法连接本机 OneAgent 服务，请确认它仍在运行", "INTERNAL_ERROR", true, 0);
  }
  let payload: T & {
    message?: string;
    error?: string;
    error_code?: string;
    retryable?: boolean;
  };
  try {
    payload = (await response.json()) as typeof payload;
  } catch {
    throw new OneAgentApiError(`服务响应异常（HTTP ${response.status}）`, "INTERNAL_ERROR", false, response.status);
  }
  if (!response.ok) {
    throw new OneAgentApiError(
      payload.message || payload.error || "OneAgent request failed",
      payload.error_code || "INTERNAL_ERROR",
      Boolean(payload.retryable),
      response.status,
    );
  }
  return payload;
}

function post<T>(path: string, body: object): Promise<T> {
  return request<T>(path, { method: "POST", body: JSON.stringify(body) });
}

export const api = {
  status: () => request<StatusResponse>("/api/status"),
  probe: (input: {
    provider: ProviderId;
    apiBaseUrl: string;
    apiKey: string;
    model: string;
    /** Selected Agents, so each one's protocol is exercised rather than
     *  assuming OpenAI Chat Completions for everything. */
    agents?: string[];
  }) =>
    post<ProbeResponse>("/api/probe", {
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      ...(input.agents?.length ? { agents: input.agents } : {}),
    }),
  models: (input: { provider: ProviderId; apiBaseUrl: string; apiKey: string }) =>
    post<ModelsResponse>("/api/models", {
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
    }),
  install: (input: InstallRequest) => post<InstallResponse>("/api/install", input),
  openRegister: (provider: Exclude<ProviderId, "custom">, agents: string[]) =>
    post<{ ok: true; url: string; message: string }>("/api/open-register", { provider, agents }),
  /** Repoint one Agent. Only that Agent's config and credential file change. */
  activateAgent: (
    agentId: string,
    input: {
      provider: ProviderId;
      apiBaseUrl: string;
      apiKey: string;
      model: string;
      profileId?: string;
      smallFastModel?: string;
    },
  ) =>
    post<ActivateAgentResponse>(`/api/agents/${encodeURIComponent(agentId)}/activate`, {
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
      ...(input.profileId ? { profile_id: input.profileId } : {}),
      ...(input.smallFastModel ? { small_fast_model: input.smallFastModel } : {}),
    }),
};
