export type TargetStatus = "available" | "verification-pending" | "planned" | "withdrawn";

export interface DownloadLink {
  id: string;
  label: string;
  kind: "official" | "mirror";
  url: string;
  primary: boolean;
}

export interface ReleaseArtifact {
  file: string;
  sha256: string;
  bytes: number;
  kind: "binary" | "source";
  downloads: DownloadLink[];
}

export interface ReleaseTarget {
  id: string;
  platform: string;
  platformLabel: string;
  arch: string;
  archLabel: string;
  status: TargetStatus;
  verification: {
    native_build: boolean;
    cleanroom: "verified" | "not-recorded" | "failed";
    evidence: string | null;
  };
  python: string | null;
  built_at: string | null;
  agent_versions: Record<string, string>;
  artifacts: ReleaseArtifact[];
}

export interface ReleaseChannel {
  channel: string;
  label: string;
  published_at: string | null;
  version: string | null;
  unsigned: boolean;
  status: "available" | "unavailable";
  targets: ReleaseTarget[];
}

export interface DetectedTarget {
  platform: "macos" | "windows" | "linux";
  arch: "arm64" | "x64" | null;
}

export interface AgentSupport {
  managedInstall: boolean;
  officialInstallGuide: boolean;
  managedConfig: boolean;
}

export function detectTargetFromUserAgent(userAgent: string): DetectedTarget | null {
  const normalized = userAgent.toLowerCase();
  if (/iphone|ipad|android/.test(normalized)) return null;
  if (/macintosh|mac os x/.test(normalized)) {
    return {
      platform: "macos",
      arch: /arm64|aarch64|apple silicon/.test(normalized) ? "arm64" : null,
    };
  }
  if (/windows/.test(normalized)) {
    return { platform: "windows", arch: /arm64|aarch64/.test(normalized) ? "arm64" : "x64" };
  }
  if (/linux|x11/.test(normalized)) {
    return { platform: "linux", arch: /arm64|aarch64/.test(normalized) ? "arm64" : "x64" };
  }
  return null;
}

export function getRecommendedTarget(channel: ReleaseChannel, detected: DetectedTarget | null): ReleaseTarget | null {
  if (detected) {
    const exact = detected.arch
      ? channel.targets.find((target) => target.platform === detected.platform && target.arch === detected.arch)
      : null;
    if (exact) return exact;
    const samePlatform = channel.targets.find(
      (target) => target.platform === detected.platform && target.status === "available",
    );
    if (samePlatform) return samePlatform;
    const anySamePlatform = channel.targets.find((target) => target.platform === detected.platform);
    if (anySamePlatform) return anySamePlatform;
  }
  return channel.targets.find((target) => target.status === "available") ?? channel.targets[0] ?? null;
}

export function binaryArtifact(target: ReleaseTarget): ReleaseArtifact | null {
  return target.artifacts.find((artifact) => artifact.kind === "binary") ?? null;
}

export function primaryDownload(artifact: ReleaseArtifact): DownloadLink | null {
  return artifact.downloads.find((download) => download.primary) ?? artifact.downloads[0] ?? null;
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KiB", "MiB", "GiB"];
  let value = bytes / 1024;
  let unit = units[0];
  for (let index = 1; index < units.length && value >= 1024; index += 1) {
    value /= 1024;
    unit = units[index];
  }
  return `${value.toFixed(1)} ${unit}`;
}

export function formatDate(value: string | null): string {
  if (!value) return "尚未发布";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
}

export function supportLabels(support: AgentSupport): string[] {
  const labels: string[] = [];
  if (support.managedInstall) labels.push("OneAgent 可管理安装");
  if (support.officialInstallGuide) labels.push("官方安装引导");
  if (support.managedConfig) labels.push("OneAgent 可管理配置");
  if (!support.managedConfig) labels.push("配置由 Agent 官方流程管理");
  return labels;
}
