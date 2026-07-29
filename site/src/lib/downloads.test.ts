import { describe, expect, it } from "vitest";
import {
  detectTargetFromUserAgent,
  formatBytes,
  getRecommendedTarget,
  supportLabels,
  type ReleaseChannel,
} from "./downloads";

const channel: ReleaseChannel = {
  channel: "technical-preview-unsigned",
  label: "未签名技术预览版",
  published_at: "2026-07-28T00:00:00Z",
  version: "0.2.0-dev",
  unsigned: true,
  status: "available",
  targets: [
    {
      id: "macos-arm64",
      platform: "macos",
      platformLabel: "macOS",
      arch: "arm64",
      archLabel: "Apple silicon / ARM64",
      status: "available",
      verification: { native_build: true, cleanroom: "verified", evidence: "security/" },
      python: "3.12.13",
      built_at: "2026-07-26T10:14:28Z",
      agent_versions: {},
      artifacts: [
        {
          file: "OneAgent.zip",
          sha256: "abc",
          bytes: 1024,
          kind: "binary",
          downloads: [{ id: "website", label: "官网下载", kind: "official", url: "downloads/OneAgent.zip", primary: true }],
        },
      ],
    },
    {
      id: "windows-x64",
      platform: "windows",
      platformLabel: "Windows",
      arch: "x64",
      archLabel: "Intel / AMD 64-bit",
      status: "verification-pending",
      verification: { native_build: false, cleanroom: "not-recorded", evidence: null },
      python: null,
      built_at: null,
      agent_versions: {},
      artifacts: [],
    },
  ],
};

describe("download targeting", () => {
  it("detects Apple silicon without hiding manual platform choices", () => {
    expect(detectTargetFromUserAgent("Mozilla/5.0 (Macintosh; Apple Silicon Mac OS X 14_5)")).toEqual({
      platform: "macos",
      arch: "arm64",
    });
  });

  it("does not pretend a generic macOS user agent reveals the chip architecture", () => {
    expect(detectTargetFromUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5)")).toEqual({
      platform: "macos",
      arch: null,
    });
  });

  it("recognizes Windows on ARM without silently recommending x64 as an exact match", () => {
    expect(detectTargetFromUserAgent("Mozilla/5.0 (Windows NT 10.0; ARM64)")).toEqual({
      platform: "windows",
      arch: "arm64",
    });
  });

  it("returns an unavailable target when it is the user's platform", () => {
    expect(getRecommendedTarget(channel, { platform: "windows", arch: "x64" })?.id).toBe("windows-x64");
  });

  it("falls back to the available artifact for an unknown browser", () => {
    expect(getRecommendedTarget(channel, null)?.id).toBe("macos-arm64");
  });

  it("formats download size without pretending to be exact decimal storage", () => {
    expect(formatBytes(1024 * 1024)).toBe("1.0 MiB");
  });
});

describe("compatibility labels", () => {
  it("keeps guide-only agents distinct from managed installation", () => {
    expect(
      supportLabels({ managedInstall: false, officialInstallGuide: true, managedConfig: false }),
    ).toEqual(["官方安装引导", "配置由 Agent 官方流程管理"]);
  });
});
