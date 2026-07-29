import { ChevronRight } from "lucide-react";

import type { AgentCatalogItem, AgentStatus, StatusResponse } from "../types/api";
import { AgentIcon, agentTagline } from "./icons/agents";
import { StatusBadge } from "./StatusBadge";

type Providers = StatusResponse["providers"];

/** -1, 0 or 1 comparing dotted numeric versions; non-numeric parts sort last. */
export function compareVersions(left: string, right: string): number {
  const parse = (value: string) => value.split(/[.+-]/).map((part) => Number.parseInt(part, 10));
  const a = parse(left);
  const b = parse(right);
  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    const x = Number.isNaN(a[i]) || a[i] === undefined ? -1 : a[i];
    const y = Number.isNaN(b[i]) || b[i] === undefined ? -1 : b[i];
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}

/** Whether the installed version is older than the locked one. */
export function isBehind(installed: string, locked: string): boolean {
  return compareVersions(installed, locked) < 0;
}

export function versionNote(status: AgentStatus): { text: string; behind: boolean } | null {
  if (!status.installed || !status.version) return null;
  if (!status.lockedVersion || status.version === status.lockedVersion) {
    // Being current is the normal case and needs no words: the bare version is
    // enough, and five rows each saying "已是最新" is noise.
    return { text: status.version, behind: false };
  }
  // Only an older local version is an update. A newer one is normal — the user
  // upgraded the Agent themselves — and calling it "updatable" would send them
  // to downgrade.
  if (compareVersions(status.version, status.lockedVersion) < 0) {
    // The arrow already says which way this goes.
    return { text: `${status.version} → ${status.lockedVersion}`, behind: true };
  }
  return { text: `${status.version}（锁定 ${status.lockedVersion}）`, behind: false };
}

/**
 * What to say this Agent currently points at.
 *
 * Two sources: OneAgent's own record of what it wrote, and the Agent's config
 * file as found on disk. Reporting only the first is what made a hand-written
 * configuration render as "未配置" — the file was seen but never read. Where they
 * disagree the file wins, because that is what the Agent will actually use.
 */
export function targetSummary(
  status: AgentStatus,
  providers: Providers,
): { text: string; note: string } {
  const detected = status.detected;
  if (detected?.unreadable) {
    return { text: "配置无法解析", note: detected.unreadable };
  }
  const providerName = status.provider ? providers[status.provider]?.name || status.provider : "";
  // Ours, and the file agrees (or has nothing to add).
  if (status.provider && status.model) {
    const drifted =
      detected && detected.baseUrl && status.baseUrl && detected.baseUrl !== status.baseUrl;
    return {
      text: `${providerName} · ${status.model}`,
      note: drifted ? `配置文件当前指向 ${detected!.baseUrl}` : "",
    };
  }
  // No record of our own, but the file says something.
  if (detected && (detected.baseUrl || detected.model)) {
    const parts = [detected.baseUrl, detected.model].filter(Boolean);
    return {
      text: parts.join(" · "),
      note: detected.managedByOneAgent ? "" : "检测到的配置，非 OneAgent 写入",
    };
  }
  return { text: "未配置", note: "" };
}

/**
 * One row in the overview: what this Agent points at, nothing editable.
 *
 * Configuration lives on /agents/:agentId. Editing inline used to grow the list
 * by the height of a whole form, let several rows sit half-configured at once,
 * and left no room for the file paths and backup state a detail view can show.
 */
export function AgentManageRow({
  agentId,
  catalog,
  status,
  providers,
  onOpen,
}: {
  agentId: string;
  catalog: AgentCatalogItem | undefined;
  status: AgentStatus;
  providers: Providers;
  onOpen: () => void;
}) {
  const version = versionNote(status);
  const target = targetSummary(status, providers);
  const configuredSomehow =
    Boolean(status.provider) || Boolean(status.detected?.baseUrl) || Boolean(status.detected?.model);
  const action = !status.installed ? "安装并配置" : configuredSomehow ? "改配置" : "配置";

  return (
    <button className="agent-manage-row" type="button" onClick={onOpen}>
      <span className="agent-icon" title={agentTagline(agentId) || undefined}>
        <AgentIcon agentId={agentId} size={18} />
      </span>
      <span className="agent-manage-identity">
        <strong>{catalog?.name || agentId}</strong>
        <span className="agent-manage-target">{target.text}</span>
        {target.note ? <small className="agent-manage-note">{target.note}</small> : null}
      </span>
      {version ? <span className={`agent-manage-version${version.behind ? " is-behind" : ""}`}>{version.text}</span> : null}
      <StatusBadge tone={status.installed ? "success" : "warning"}>
        {status.installed ? "已安装" : "待安装"}
      </StatusBadge>
      <span className="agent-manage-cta">
        {action}
        <ChevronRight size={15} aria-hidden="true" />
      </span>
    </button>
  );
}
