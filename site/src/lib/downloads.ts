export interface ReleaseTarget {
  id: string;
  platform: "macos" | "windows" | "linux";
  platformLabel: string;
  arch: "arm64" | "x64";
  archLabel: string;
  file: string;
  bytes: number;
  sha256: string | null;
  downloadUrl: string;
  checksumUrl: string | null;
}

interface GitHubReleaseAsset {
  name: string;
  size: number;
  digest?: string | null;
  browser_download_url: string;
}

export interface GitHubRelease {
  name: string | null;
  tag_name: string;
  html_url: string;
  published_at: string | null;
  prerelease: boolean;
  draft: boolean;
  assets: GitHubReleaseAsset[];
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

const repository = process.env.GITHUB_REPOSITORY || "MaimoryLab/OneAgent";
const releasesUrl = `https://api.github.com/repos/${repository}/releases?per_page=20`;
export const releasesPageUrl = `https://github.com/${repository}/releases`;
let releasesRequest: Promise<GitHubRelease[]> | undefined;

function getPublishedReleases(): Promise<GitHubRelease[]> {
  releasesRequest ??= fetch(releasesUrl, {
    headers: {
      Accept: "application/vnd.github+json",
      "X-GitHub-Api-Version": "2022-11-28",
      ...(process.env.GITHUB_TOKEN ? { Authorization: `Bearer ${process.env.GITHUB_TOKEN}` } : {}),
    },
  }).then(async (response) => {
    if (response.status === 404 && !process.env.GITHUB_TOKEN) return [];
    if (!response.ok) throw new Error(`GitHub Releases request failed: ${response.status} ${response.statusText}`);
    const releases = await response.json();
    if (!Array.isArray(releases)) throw new Error("GitHub Releases response must be an array");
    return (releases as GitHubRelease[]).filter((release) => !release.draft);
  });
  return releasesRequest;
}

export async function getLatestRelease(): Promise<GitHubRelease | null> {
  return (await getPublishedReleases())[0] ?? null;
}

const platformLabels = { macos: "macOS", windows: "Windows", linux: "Linux" } as const;
const archLabels = { arm64: "Apple silicon / ARM64", x64: "Intel / AMD 64-bit" } as const;
const binarySuffixes = [".zip", ".tar.gz", ".dmg", ".msi", ".exe", ".appimage"];

export function releaseTargets(release: GitHubRelease): ReleaseTarget[] {
  return release.assets.flatMap((asset) => {
    const match = asset.name.toLowerCase().match(/-(macos|windows|linux)-(arm64|x64)(?:\.[a-z0-9.]+)$/);
    if (!match || !binarySuffixes.some((suffix) => asset.name.toLowerCase().endsWith(suffix))) return [];
    const platform = match[1] as ReleaseTarget["platform"];
    const arch = match[2] as ReleaseTarget["arch"];
    const digest = asset.digest?.match(/^sha256:([a-f0-9]{64})$/i)?.[1].toLowerCase() ?? null;
    const checksum = release.assets.find((candidate) => candidate.name === `SHA256SUMS-${platform}-${arch}.txt`);
    return [{
      id: `${platform}-${arch}`,
      platform,
      platformLabel: platformLabels[platform],
      arch,
      archLabel: archLabels[arch],
      file: asset.name,
      bytes: asset.size,
      sha256: digest,
      downloadUrl: asset.browser_download_url,
      checksumUrl: checksum?.browser_download_url ?? null,
    }];
  });
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
