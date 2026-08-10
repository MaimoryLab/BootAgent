import { describe, expect, it } from "vitest";

import { sourceTranslate } from "../i18n";
import { failureCopyFor, isHTTPStatus, runtimeDownloadHint } from "./failureCopy";

// sourceTranslate returns the Chinese keys, which is what the source language is.
const t = sourceTranslate;

describe("failureCopyFor", () => {
  it("separates the two statuses a new user hits most", () => {
    // Both used to arrive as a bare "Endpoint returned HTTP 429." with no
    // indication of which, or that the account rather than the config was at fault.
    expect(failureCopyFor("PROVIDER_UNREACHABLE", 429, t)?.message).toBe("请求过于频繁，或已达到额度上限");
    expect(failureCopyFor("PROVIDER_UNREACHABLE", 402, t)?.message).toBe("账户额度不足");
  });

  it("distinguishes a timeout from an unreachable endpoint", () => {
    // The backend tracks these as separate codes while producing the identical
    // "Cannot reach endpoint" sentence for both.
    const timeout = failureCopyFor("TIMEOUT", 0, t);
    const unreachable = failureCopyFor("PROVIDER_UNREACHABLE", 0, t);
    expect(timeout?.message).toBe("连接模型服务超时");
    expect(unreachable?.message).toBe("无法连接到模型服务");
    expect(timeout?.message).not.toBe(unreachable?.message);
  });

  it("reads a 401 behind PROVIDER_UNREACHABLE as a rejected key", () => {
    // classifyHTTPModels only tags API_KEY_REJECTED when the models call failed,
    // so the same rejection arrives under either code depending on the path.
    expect(failureCopyFor("PROVIDER_UNREACHABLE", 401, t)?.message)
      .toBe(failureCopyFor("API_KEY_REJECTED", 401, t)?.message);
  });

  it("treats status 0 as a request that never landed", () => {
    // internal/provider/client.go:21 uses 0 as the sentinel, so it must not be
    // read as an HTTP response.
    expect(isHTTPStatus(0)).toBe(false);
    expect(isHTTPStatus(undefined)).toBe(false);
    expect(isHTTPStatus(429)).toBe(true);
    // Falls back to the code's own copy rather than inventing an HTTP sentence.
    expect(failureCopyFor("PROVIDER_UNREACHABLE", 0, t)?.message).toBe("无法连接到模型服务");
  });

  it("returns null where the code cannot improve on the backend message", () => {
    // INVALID_REQUEST is field-level validation text; replacing it with a generic
    // sentence would lose what the user needs to fix.
    expect(failureCopyFor("INVALID_REQUEST", 400, t)).toBeNull();
    expect(failureCopyFor("INTERNAL_ERROR", 500, t)).toBeNull();
    expect(failureCopyFor(null, undefined, t)).toBeNull();
    expect(failureCopyFor(undefined, undefined, t)).toBeNull();
  });

  it("names a server-side failure as the provider's problem", () => {
    const copy = failureCopyFor("PROVIDER_UNREACHABLE", 503, t);
    expect(copy?.message).toContain("503");
    expect(copy?.hint).toBe("这是对方服务的问题，稍后重试");
  });

  it("gives every mapped code something to do about it", () => {
    // A message that only restates the problem leaves the user where they started;
    // these are the codes where there is a next step to name.
    for (const code of [
      "API_KEY_REJECTED",
      "PROVIDER_UNREACHABLE",
      "TIMEOUT",
      "PROTOCOL_UNSUPPORTED",
      "MODELS_UNSUPPORTED",
      "CONFIG_WRITE_FAILED",
      "UPDATE_NOT_INSTALLABLE",
    ]) {
      const copy = failureCopyFor(code, 0, t);
      expect(copy, code).not.toBeNull();
      expect(copy?.hint, code).toBeTruthy();
    }
  });

  it("never leaks a raw transport detail into the copy", () => {
    // The whole point: "dial tcp: lookup api.example.com: no such host" is written
    // for a maintainer reading a log.
    for (const status of [0, 401, 402, 404, 429, 500, 503, 418]) {
      const copy = failureCopyFor("PROVIDER_UNREACHABLE", status, t);
      expect(copy?.message ?? "").not.toMatch(/dial tcp|Post "|no such host|context deadline/);
    }
  });
});

describe("UPDATE_NOT_INSTALLABLE", () => {
  // A .dmg reached the helper unextracted and replaced OneAgent.app with the disk
  // image, leaving an installation that could not launch. The backend now refuses
  // the artifact, and this is the one code whose hint must not suggest retrying:
  // another attempt downloads the same asset.
  it("sends the user to a manual download rather than a retry", () => {
    const copy = failureCopyFor("UPDATE_NOT_INSTALLABLE", 500, t);
    expect(copy?.message).toBe("下载到的更新包无法安装");
    expect(copy?.hint).toContain("手动下载");
    expect(copy?.hint).not.toMatch(/重试|再试/);
  });
});

describe("runtimeDownloadHint", () => {
  // Counter-intuitive on purpose: downloadSources always tries both hosts and the
  // preference only reorders them, so by the time a download fails the mirror has
  // been attempted and the setting a user would reach for cannot change anything.
  it("says switching the download source will not help", () => {
    const hint = runtimeDownloadHint(t);
    expect(hint).toContain("都已尝试过");
    expect(hint).toContain("不会改变结果");
  });
});
