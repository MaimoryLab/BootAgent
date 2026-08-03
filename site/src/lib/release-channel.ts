/**
 * Adapts GitHub Releases into the channel shape the pages render.
 *
 * The pages were written against a locally generated release index that carried
 * per-target build provenance: which Python built it, whether the build was
 * native, whether a cleanroom run passed. GitHub Releases carries none of that —
 * it knows an asset's name, size and digest, and nothing about how it was made.
 *
 * So this module maps what the API does return and reports the rest as unknown
 * rather than filling it in. `native_build: false` and `cleanroom:
 * "not-recorded"` are the honest readings of "the release feed cannot tell us",
 * and the pages already render that as an absence of evidence instead of a
 * claim. Inventing a `true` here would put a verification badge on the site that
 * nothing checked.
 */
import {
  type DetectedTarget,
  type GitHubRelease,
  type ReleaseTarget as AssetTarget,
  getLatestRelease,
  releaseTargets,
} from "./downloads";

export type { DetectedTarget };

type TargetStatus = "available" | "verification-pending" | "planned" | "withdrawn";

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

/* Every platform/arch OneAgent intends to ship, in the order the download page
   lists them. Targets with no asset in the release still appear, as `planned` —
   a reader comparing platforms should see that Windows exists and is not ready
   yet, rather than find it missing and wonder. */
const plannedTargets: Array<{ id: string; platform: string; platformLabel: string; arch: string; archLabel: string }> = [
  { id: "macos-arm64", platform: "macos", platformLabel: "macOS", arch: "arm64", archLabel: "Apple silicon / ARM64" },
  { id: "macos-x64", platform: "macos", platformLabel: "macOS", arch: "x64", archLabel: "Intel / AMD 64-bit" },
  { id: "windows-x64", platform: "windows", platformLabel: "Windows", arch: "x64", archLabel: "Intel / AMD 64-bit" },
  { id: "linux-x64", platform: "linux", platformLabel: "Linux", arch: "x64", archLabel: "Intel / AMD 64-bit" },
];

const previewChannelId = "technical-preview-unsigned";

/* The tag carries the channel. A prerelease tag, or one saying so in its name,
   is the unsigned technical preview; anything else is a signed stable build.
   Read from the release rather than hardcoded so the site follows the tag
   instead of needing an edit on the day Stable ships. */
function channelOf(release: GitHubRelease): { channel: string; label: string; unsigned: boolean } {
  const preview = release.prerelease || /preview|unsigned|dev|rc/i.test(release.tag_name);
  return preview
    ? { channel: previewChannelId, label: "未签名技术预览版", unsigned: true }
    : { channel: "stable", label: "稳定版", unsigned: false };
}

function artifactFor(asset: AssetTarget): ReleaseArtifact[] {
  /* A missing digest means the asset predates GitHub's per-asset digests. The
     checksum file is still linked, so a reader can verify by hand — but the page
     must not print a checksum that was never published. */
  if (!asset.sha256) return [];
  return [{
    file: asset.file,
    sha256: asset.sha256,
    bytes: asset.bytes,
    kind: "binary",
    downloads: [{ id: "github", label: "GitHub Releases", kind: "official", url: asset.downloadUrl, primary: true }],
  }];
}

/**
 * Builds the channel the pages render from the newest published release.
 *
 * Returns null when the release feed is empty or unreachable, which is the state
 * a fresh fork is in. Callers render the "not published yet" copy for null
 * rather than failing the build, because a site that cannot be built without a
 * network round trip cannot be built in CI.
 */
export async function getPreviewChannel(): Promise<ReleaseChannel | null> {
  const release = await getLatestRelease();
  if (!release) return null;
  const assets = releaseTargets(release);
  const { channel, label, unsigned } = channelOf(release);
  const targets: ReleaseTarget[] = plannedTargets.map((planned) => {
    const asset = assets.find((candidate) => candidate.id === planned.id);
    const artifacts = asset ? artifactFor(asset) : [];
    return {
      ...planned,
      /* An asset whose digest never got published is `verification-pending`, not
         `available`: the download page's whole claim is that you can check what
         you downloaded, and without a checksum you cannot. */
      status: !asset ? "planned" : artifacts.length > 0 ? "available" : "verification-pending",
      verification: { native_build: false, cleanroom: "not-recorded", evidence: "security/#release-evidence" },
      python: null,
      built_at: release.published_at,
      artifacts,
    };
  });
  return {
    channel,
    label,
    published_at: release.published_at,
    version: release.tag_name.replace(/^v/, ""),
    unsigned,
    status: targets.some((target) => target.status === "available") ? "available" : "unavailable",
    targets,
  };
}
export function recommendedTargetIn(channel: ReleaseChannel, detected: DetectedTarget | null): ReleaseTarget | null {
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
