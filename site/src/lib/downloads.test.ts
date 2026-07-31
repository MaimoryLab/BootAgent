import { describe, expect, it } from "vitest";
import {
  detectTargetFromUserAgent,
  getRecommendedTarget,
  releaseTargets,
  supportLabels,
  type GitHubRelease,
} from "./downloads";

const release: GitHubRelease = {
  name: "OneAgent preview",
  tag_name: "v0.3.0-preview.1",
  html_url: "https://github.com/MaimoryLab/OneAgent/releases/tag/v0.3.0-preview.1",
  published_at: "2026-07-31T00:00:00Z",
  prerelease: true,
  draft: false,
  assets: [
    {
      name: "OneAgent-0.3.0-technical-preview-unsigned-macos-arm64.zip",
      size: 1024,
      digest: `sha256:${"a".repeat(64)}`,
      browser_download_url: "https://github.com/MaimoryLab/OneAgent/releases/download/v0.3.0-preview.1/OneAgent.zip",
    },
    {
      name: "SHA256SUMS-macos-arm64.txt",
      size: 128,
      browser_download_url: "https://github.com/MaimoryLab/OneAgent/releases/download/v0.3.0-preview.1/SHA256SUMS.txt",
    },
    {
      name: "release-manifest-macos-arm64.json",
      size: 256,
      browser_download_url: "https://github.com/MaimoryLab/OneAgent/releases/download/v0.3.0-preview.1/manifest.json",
    },
  ],
};

describe("GitHub Release downloads", () => {
  it("derives downloadable platforms and checksums only from release assets", () => {
    const targets = releaseTargets(release);
    expect(targets).toHaveLength(1);
    expect(targets[0]).toMatchObject({
      id: "macos-arm64",
      sha256: "a".repeat(64),
      checksumUrl: expect.stringContaining("SHA256SUMS"),
    });
  });

  it("uses the browser platform without hiding the other release assets", () => {
    const detected = detectTargetFromUserAgent("Mozilla/5.0 (Macintosh; Apple Silicon Mac OS X 14_5)");
    expect(detected).toEqual({ platform: "macos", arch: "arm64" });
    expect(getRecommendedTarget(releaseTargets(release), detected)?.id).toBe("macos-arm64");
  });
});

describe("compatibility labels", () => {
  it("keeps guide-only agents distinct from managed installation", () => {
    expect(supportLabels({ managedInstall: false, officialInstallGuide: true, managedConfig: false })).toEqual([
      "官方安装引导",
      "配置由 Agent 官方流程管理",
    ]);
  });
});
