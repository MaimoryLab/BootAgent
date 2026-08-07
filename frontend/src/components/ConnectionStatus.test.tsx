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
    expect(screen.getByText("模型请求被拒绝")).toBeTruthy();
    expect(screen.queryByText(/OneAgent chose the model/)).toBeNull();
  });

  it("does not blame the model when the key itself was rejected", () => {
    // A rejected key is about the key whatever model carried the request.
    show(probe({ error_code: "API_KEY_REJECTED", message: "API Key 无效", model: "wan-ai/wan2.1-t2v-14b", auto_selected_model: true }));
    expect(screen.getByText("API Key 无效")).toBeTruthy();
    expect(screen.queryByText(/OneAgent chose the model/)).toBeNull();
  });

  it("reports a successful probe with the provider's own message", () => {
    show(probe({ ok: true, status: 200, message: "连接正常", error_code: null }));
    expect(screen.getByText("连接正常")).toBeTruthy();
  });
});
