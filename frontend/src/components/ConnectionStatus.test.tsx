import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { I18nProvider } from "../i18n";
import type { ProbeResponse } from "../types/api";
import { ConnectionStatus } from "./ConnectionStatus";

function probe(overrides: Partial<ProbeResponse> = {}): ProbeResponse {
  return {
    ok: false, reachable: true, status: 400, message: "模型请求被拒绝",
    error_code: "PROTOCOL_UNSUPPORTED", retryable: false, ...overrides,
  } as ProbeResponse;
}

// jsdom reports navigator.language as en-US, so I18nProvider resolves to English
// and the assertions below match the translated strings rather than the zh keys.
function show(result: ProbeResponse) {
  render(<I18nProvider><ConnectionStatus state="success" result={result} /></I18nProvider>);
}

describe("ConnectionStatus", () => {
  // A Provider's catalogue is mostly image, video and audio generators. One of
  // those rejecting a chat payload used to be indistinguishable from a bad key,
  // so the user read a wrong verdict about their credentials during setup.
  it("names a model it chose itself when the probe fails", () => {
    show(probe({ model: "wan-ai/wan2.1-t2v-14b", auto_selected_model: true }));
    expect(screen.getByText(/wan-ai\/wan2\.1-t2v-14b/)).toBeTruthy();
    expect(screen.getByText(/OneAgent chose the model/)).toBeTruthy();
  });

  it("stays quiet about the model the user chose themselves", () => {
    // Their override, so the failure is the answer they asked for; explaining our
    // choice would be both wrong and confusing.
    show(probe({ model: "kwai/kling-v1-video", auto_selected_model: false }));
    expect(screen.getByText(/does not serve the selected API type/)).toBeTruthy();
    expect(screen.queryByText(/OneAgent chose the model/)).toBeNull();
  });

  it("does not blame the model when the key itself was rejected", () => {
    // A rejected key is about the key whatever model carried the request.
    show(probe({ error_code: "API_KEY_REJECTED", status: 401, model: "wan-ai/wan2.1-t2v-14b", auto_selected_model: true }));
    expect(screen.getByText("The API key was rejected")).toBeTruthy();
    expect(screen.queryByText(/OneAgent chose the model/)).toBeNull();
  });

  it("reports a successful probe with the provider's own message", () => {
    show(probe({ ok: true, status: 200, message: "连接正常", error_code: null }));
    expect(screen.getByText("连接正常")).toBeTruthy();
  });

  // The backend's own sentences are English and written for a maintainer reading a
  // log. These replace them rather than appending, so the user is not left
  // deciding which half to trust.
  it("replaces the English backend message with localised copy", () => {
    show(probe({ error_code: "PROVIDER_UNREACHABLE", status: 0, message: 'Cannot reach endpoint: Post "https://api.example.test/v1/chat/completions": dial tcp: lookup api.example.test: no such host' }));
    expect(screen.getByText("Could not reach the provider")).toBeTruthy();
    expect(screen.queryByText(/dial tcp/)).toBeNull();
    expect(screen.getByText(/Check the network connection and the base URL/)).toBeTruthy();
  });

  // 429 and 402 were the two a new user hits most, and both arrived as a bare
  // "Endpoint returned HTTP 429." with no indication of which, or that the
  // account rather than the configuration was the problem.
  it("explains a rate limit and an exhausted balance separately", () => {
    show(probe({ error_code: "PROVIDER_UNREACHABLE", status: 429, message: "Endpoint returned HTTP 429." }));
    expect(screen.getByText(/Too many requests, or a quota limit/)).toBeTruthy();

    show(probe({ error_code: "PROVIDER_UNREACHABLE", status: 402, message: "Endpoint returned HTTP 402." }));
    expect(screen.getByText("The account is out of credit")).toBeTruthy();
  });

  // The backend has always tracked TIMEOUT separately from PROVIDER_UNREACHABLE
  // while producing the identical "Cannot reach endpoint" sentence for both, so a
  // 10-second hang and a mistyped hostname looked the same.
  it("distinguishes a timeout from an unreachable endpoint", () => {
    show(probe({ error_code: "TIMEOUT", status: 0, message: "Cannot reach endpoint: context deadline exceeded" }));
    expect(screen.getByText("The connection to the provider timed out")).toBeTruthy();
  });

  it("treats a 401 behind PROVIDER_UNREACHABLE as a rejected key", () => {
    // classifyHTTPModels only tags API_KEY_REJECTED when the models call is what
    // failed, so the status has to be read too or the same rejection renders as a
    // hard error on one path and a warning on the other.
    show(probe({ error_code: "PROVIDER_UNREACHABLE", status: 401, message: "Endpoint returned HTTP 401." }));
    expect(screen.getByText("The API key was rejected")).toBeTruthy();
    expect(document.querySelector(".status-warning")).toBeTruthy();
  });

  it("keeps a message the code cannot improve on", () => {
    // INVALID_REQUEST carries field-level detail written for the field it came
    // from; a generic sentence would lose what the user needs.
    show(probe({ error_code: "INVALID_REQUEST", status: 400, message: "Provider name is required" }));
    expect(screen.getByText("Provider name is required")).toBeTruthy();
  });
});
