import { afterEach, describe, expect, it, vi } from "vitest";

import { api, OneAgentApiError } from "./client";

function jsonResponse(payload: object, status = 200) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api client", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("loads status with same-origin credentials", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ apiVersion: 1 }));
    await expect(api.status()).resolves.toEqual({ apiVersion: 1 });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/status",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("maps structured server errors", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ message: "bad origin", error_code: "INVALID_ORIGIN", retryable: false }, 403),
    );
    await expect(api.status()).rejects.toMatchObject({
      message: "bad origin",
      code: "INVALID_ORIGIN",
      retryable: false,
      status: 403,
    });
  });

  it("posts provider requests using API field names", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false }),
    );
    await api.probe({ provider: "custom", apiBaseUrl: "http://127.0.0.1:9000", apiKey: "sentinel", model: "model-a" });
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(String(init.body))).toEqual({
      provider: "custom",
      api_base_url: "http://127.0.0.1:9000",
      api_key: "sentinel",
      model: "model-a",
    });
  });

  it("supports models, install and register endpoints", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ ok: true, models: ["a"] }))
      .mockResolvedValueOnce(jsonResponse({ ok: true, code: 0, results: [], log: "", next: "", probe: null }))
      .mockResolvedValueOnce(jsonResponse({ ok: true, url: "https://ppio.com/", message: "opened" }));

    await api.models({ provider: "ppio", apiBaseUrl: "", apiKey: "key" });
    await api.install({
      agents: ["codex"],
      provider: "ppio",
      api_key: "key",
      model: "model-a",
      configure: true,
      install_agent: false,
      skip_test: true,
    });
    await api.openRegister("ppio", ["codex"]);

    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual(["/api/models", "/api/install", "/api/open-register"]);
  });
});
