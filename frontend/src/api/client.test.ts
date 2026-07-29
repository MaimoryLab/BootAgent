import { afterEach, describe, expect, it, vi } from "vitest";

import { api, describeError, OneAgentApiError } from "./client";

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

  it("falls back through error, then a generic message", async () => {
    // A proxy or a crash can return a body without the structured fields; the
    // wizard must still surface something actionable rather than "undefined".
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ error: "boom" }, 500));
    await expect(api.status()).rejects.toMatchObject({
      message: "boom",
      code: "INTERNAL_ERROR",
      retryable: false,
      status: 500,
    });
  });

  it("survives an error body with no recognisable fields", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({}, 502));
    await expect(api.status()).rejects.toMatchObject({
      message: "OneAgent request failed",
      code: "INTERNAL_ERROR",
      status: 502,
    });
  });

  it("wraps a network failure instead of leaking the raw TypeError", async () => {
    // The common cause is the local GUI process having exited; the user must
    // see an actionable Chinese message, not "Failed to fetch".
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(api.status()).rejects.toMatchObject({
      name: "OneAgentApiError",
      message: expect.stringContaining("无法连接本机 OneAgent 服务"),
      retryable: true,
    });
  });

  it("wraps a non-JSON response body", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("<html>proxy error</html>", { status: 502, headers: { "Content-Type": "text/html" } }),
    );
    await expect(api.status()).rejects.toMatchObject({
      name: "OneAgentApiError",
      message: expect.stringContaining("HTTP 502"),
      status: 502,
    });
  });

  it("describes errors preserving the API contract, with a fallback otherwise", () => {
    const apiError = new OneAgentApiError("key rejected", "API_KEY_REJECTED", false, 401);
    expect(describeError(apiError, "fallback")).toEqual({
      message: "key rejected",
      code: "API_KEY_REJECTED",
      retryable: false,
    });
    expect(describeError(new Error("boom"), "fallback")).toEqual({
      message: "boom",
      code: "INTERNAL_ERROR",
      retryable: true,
    });
    expect(describeError("not-an-error", "fallback")).toEqual({
      message: "fallback",
      code: "INTERNAL_ERROR",
      retryable: true,
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

  it("sends the selected agents so each protocol is probed", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false }),
    );
    await api.probe({
      provider: "custom",
      apiBaseUrl: "http://127.0.0.1:9000",
      apiKey: "sentinel",
      model: "model-a",
      agents: ["codex", "opencode"],
    });
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(init.body)).agents).toEqual(["codex", "opencode"]);
  });

  it("omits agents entirely when none are selected", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ ok: true, reachable: true, status: 200, message: "ok", error_code: null, retryable: false }),
    );
    await api.probe({ provider: "ppio", apiBaseUrl: "", apiKey: "sentinel", model: "m", agents: [] });
    const body = JSON.parse(String((fetchMock.mock.calls[0][1] as RequestInit).body));
    expect(body).not.toHaveProperty("agents");
  });

  it("includes small_fast_model on activate only when provided", async () => {
    // A fresh response per call: a Response body can only be read once, and this
    // test exercises activate twice.
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() =>
      Promise.resolve(
        jsonResponse({ ok: true, agent: "claude-code", config: "/c", provider: "ppio", model: "m", restart: "r", next: "n" }),
      ),
    );
    await api.activateAgent("claude-code", {
      provider: "ppio",
      apiBaseUrl: "",
      apiKey: "sentinel",
      model: "model-a",
      smallFastModel: "model-fast",
    });
    let body = JSON.parse(String((fetchMock.mock.calls[0][1] as RequestInit).body));
    expect(body.small_fast_model).toBe("model-fast");

    // Empty falls back to the main model on the backend, so the field is omitted
    // rather than sent blank.
    await api.activateAgent("claude-code", {
      provider: "ppio",
      apiBaseUrl: "",
      apiKey: "sentinel",
      model: "model-a",
    });
    body = JSON.parse(String((fetchMock.mock.calls[1][1] as RequestInit).body));
    expect(body).not.toHaveProperty("small_fast_model");
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
