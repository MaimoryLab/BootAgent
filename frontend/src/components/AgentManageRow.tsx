import { Play, RefreshCw, SlidersHorizontal } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { sourceTranslate, type Translate, useI18n } from "../i18n";
import type { AgentCatalogItem, AgentStatus, ProfileSummary, StatusResponse } from "../types/api";
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

export function versionNote(status: AgentStatus, t: Translate = sourceTranslate): { text: string; behind: boolean } | null {
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
  return { text: t("{version}（锁定 {lockedVersion}）", { version: status.version, lockedVersion: status.lockedVersion }), behind: false };
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
  t: Translate = sourceTranslate,
): { text: string; note: string } {
  const detected = status.detected;
  if (detected?.unreadable) {
    return { text: t("配置无法解析"), note: detected.unreadable };
  }
  const providerName = status.provider ? providers[status.provider]?.name || status.provider : "";
  // Ours, and the file agrees (or has nothing to add).
  if (status.provider && status.model) {
    const drifted =
      detected && detected.baseUrl && status.baseUrl && detected.baseUrl !== status.baseUrl;
    return {
      text: `${providerName} · ${status.model}`,
      note: drifted ? t("配置文件当前指向 {url}", { url: detected!.baseUrl }) : "",
    };
  }
  // No record of our own, but the file says something.
  if (detected && (detected.baseUrl || detected.model)) {
    const parts = [detected.baseUrl, detected.model].filter(Boolean);
    return {
      text: parts.join(" · "),
      note: detected.managedByOneAgent ? "" : t("检测到的配置，非 OneAgent 写入"),
    };
  }
  return { text: t("未配置"), note: "" };
}

/**
 * One read-only row in the environment overview.
 */
export function AgentManageRow({
  agentId,
  catalog,
  status,
  providers,
  profileName,
  profile,
}: {
  agentId: string;
  catalog: AgentCatalogItem | undefined;
  status: AgentStatus;
  providers: Providers;
  profileName: string;
  profile?: ProfileSummary;
}) {
  const { t } = useI18n();
  const [launching, setLaunching] = useState(false);
  const [failure, setFailure] = useState("");
  const version = versionNote(status, t);
  const target = targetSummary(status, providers, t);
  const providerId = profile?.provider || status.provider || "";
  const providerName = providerId
    ? providers[providerId]?.name || providerId
    : status.detected?.baseUrl || t("未记录");
  const model = profile?.model || status.model || status.detected?.model || t("未记录");

  // installed is true only when the Agent's command resolved on the managed
  // PATH, so it is already the precise "there is something to launch" signal.
  const canLaunch = status.installed;

  const launch = async () => {
    setLaunching(true);
    setFailure("");
    try {
      await api.launchAgent(agentId);
    } catch (error) {
      setFailure(describeError(error, t("无法启动 Agent")).message);
    } finally {
      setLaunching(false);
    }
  };

  return (
    <div className="agent-manage-row" data-testid={`agent-${agentId}`}>
      <span className="agent-icon" title={agentTagline(agentId, t) || undefined}>
        <AgentIcon agentId={agentId} size={18} />
      </span>
      <span className="agent-manage-identity">
        <strong>{catalog?.name || agentId}</strong>
        {failure ? <small className="agent-manage-note is-error">{failure}</small> : null}
        {target.note ? <small className="agent-manage-note">{target.note}</small> : null}
      </span>
      <span className="agent-manage-fact"><small>Provider</small><span title={providerName}>{providerName}</span></span>
      <span className="agent-manage-fact"><small>Profile</small><span title={status.profileId || undefined}>{profileName || t("未绑定")}</span></span>
      <span className="agent-manage-fact"><small>{t("模型")}</small><span title={model}>{model}</span></span>
      <span className={`agent-manage-fact agent-manage-version${version?.behind ? " is-behind" : ""}`}>
        <small>{t("版本")}</small><span>{version?.text || t("未知")}</span>
      </span>
      <StatusBadge tone={status.installed ? "success" : "warning"}>
        {status.installed ? t("已安装") : t("待安装")}
      </StatusBadge>
      <span className="agent-manage-actions">
        {/* An explicit button rather than making the whole row a link: the row
            already carries the launch action, and two competing click targets in
            one row is how you launch an Agent while meaning to configure it. */}
        <Link
          className="button button-secondary"
          to={`/agents/${agentId}`}
          title={t("编辑这个 Agent 关联的 Profile")}
        >
          <SlidersHorizontal size={15} />
          {t("编辑配置")}
        </Link>
        {canLaunch ? (
          <button
            className="button button-secondary"
            type="button"
            onClick={() => void launch()}
            disabled={launching}
            title={t("在新终端窗口中启动，并载入 OneAgent 写入的配置")}
          >
            {launching ? <RefreshCw size={15} className="spin" /> : <Play size={15} />}
            {t("启动")}
          </button>
        ) : null}
      </span>
    </div>
  );
}
