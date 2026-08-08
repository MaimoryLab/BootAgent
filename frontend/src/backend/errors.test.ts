import { describe, expect, it } from "vitest";

import { sourceTranslate } from "../i18n";
import { describeError, describeFailure, OneAgentApiError } from "./errors";

const t = sourceTranslate;

/** What normalizeWailsError produces for a backend failure. */
const apiError = (message: string, code: string, status = 400) =>
  new OneAgentApiError(message, code, false, status);

describe("describeFailure", () => {
  // The defect this exists for: describeError returns error.message verbatim, and
  // because normalizeWailsError wraps every backend failure in OneAgentApiError,
  // the Chinese t() fallbacks callers passed almost never fired. Users read
  // English inside a Chinese UI.
  it("replaces the English backend message with localised copy", () => {
    const raw = 'Cannot reach endpoint: Post "https://api.example.test/v1/chat/completions": dial tcp: lookup api.example.test: no such host';
    const error = apiError(raw, "PROVIDER_UNREACHABLE", 0);

    expect(describeError(error, "fallback").message).toBe(raw);
    expect(describeFailure(error, "fallback", t).message).toBe("无法连接到模型服务");
  });

  it("carries the hint through so the caller can render a next step", () => {
    const detail = describeFailure(apiError("Endpoint returned HTTP 429.", "PROVIDER_UNREACHABLE", 429), "fallback", t);
    expect(detail.message).toBe("请求过于频繁，或已达到额度上限");
    expect(detail.hint).toBeTruthy();
  });

  it("preserves the code and the retryable flag", () => {
    // The retry button and the status styling read these, so localising the
    // message must not disturb them.
    const error = new OneAgentApiError("Cannot write temporary file for /c: permission denied", "CONFIG_WRITE_FAILED", true, 400);
    const detail = describeFailure(error, "fallback", t);
    expect(detail.code).toBe("CONFIG_WRITE_FAILED");
    expect(detail.retryable).toBe(true);
    expect(detail.message).toBe("无法写入配置文件");
  });

  it("keeps a message no code can improve on", () => {
    // INVALID_REQUEST is validation text written for the field it came from.
    const detail = describeFailure(apiError("Provider name is required", "INVALID_REQUEST"), "fallback", t);
    expect(detail.message).toBe("Provider name is required");
    expect(detail.hint).toBeUndefined();
  });

  it("falls back for a thrown value that is not a backend error", () => {
    expect(describeFailure("just a string", "回退文案", t).message).toBe("回退文案");
    // A real Error keeps its own message: this is where a frontend bug surfaces,
    // and replacing it with a generic sentence would hide it.
    expect(describeFailure(new Error("boom"), "回退文案", t).message).toBe("boom");
  });

  it("exposes the status describeError previously dropped", () => {
    // Without it the copy layer could not tell 429 from 402 behind one code.
    expect(describeError(apiError("x", "PROVIDER_UNREACHABLE", 402), "f").status).toBe(402);
  });
});
