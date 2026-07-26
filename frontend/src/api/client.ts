import type {
  InstallRequest,
  InstallResponse,
  ModelsResponse,
  ProbeResponse,
  ProviderId,
  StatusResponse,
} from "../types/api";

export class OneAgentApiError extends Error {
  readonly code: string;
  readonly retryable: boolean;
  readonly status: number;

  constructor(message: string, code: string, retryable: boolean, status: number) {
    super(message);
    this.name = "OneAgentApiError";
    this.code = code;
    this.retryable = retryable;
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  const payload = (await response.json()) as T & {
    message?: string;
    error?: string;
    error_code?: string;
    retryable?: boolean;
  };
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
  probe: (input: { provider: ProviderId; apiBaseUrl: string; apiKey: string; model: string }) =>
    post<ProbeResponse>("/api/probe", {
      provider: input.provider,
      api_base_url: input.apiBaseUrl,
      api_key: input.apiKey,
      model: input.model,
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
};
