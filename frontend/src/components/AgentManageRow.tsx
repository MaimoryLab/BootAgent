import { RefreshCw } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { sourceTranslate, type Translate, useI18n } from "../i18n";
import type { AgentCatalogItem, AgentStatus, ProfileSummary, StatusResponse } from "../types/api";
import { AgentIcon, agentTagline } from "./icons/agents";

type Providers = StatusResponse["providers"];
const npmAgents = new Set(["codex", "claude-code", "opencode", "kilo-cli"]);
const npmPackages: Record<string, string> = {
  codex: "@openai/codex",
  "claude-code": "@anthropic-ai/claude-code",
  opencode: "opencode-ai",
  "kilo-cli": "@kilocode/cli",
};

const AGENT_ACCENTS: Record<string, "blue" | "green" | "orange"> = {
  codex: "blue",
  "claude-code": "orange",
  opencode: "green",
  "kilo-cli": "blue",
  aider: "orange",
};

function agentAccent(agentId: string): "blue" | "green" | "orange" {
  return AGENT_ACCENTS[agentId] || "blue";
}

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
  onChanged,
}: {
  agentId: string;
  catalog: AgentCatalogItem | undefined;
  status: AgentStatus;
  providers: Providers;
  profileName: string;
  profile?: ProfileSummary;
  onChanged?: () => void | Promise<void>;
}) {
  const { t } = useI18n();
  const [launching, setLaunching] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [failure, setFailure] = useState("");
  const version = versionNote(status, t);
  const target = targetSummary(status, providers, t);
  const providerId = profile?.provider || status.provider || "";
  const providerName = providerId ? providers[providerId]?.name || providerId : "";
  const profileLabel = profileName || status.profileId || "";
  const model = profile?.model || status.model || status.detected?.model || "";
  const baseUrl = status.baseUrl || status.detected?.baseUrl || "";
  const hasConfiguration = Boolean(providerName || profileLabel || model || baseUrl);
  const statusLabel = failure ? t("失败") : !status.installed ? t("未安装") : !hasConfiguration ? t("未配置") : "";

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
  const update = async () => {
    setUpdating(true);
    setFailure("");
    try {
      await api.updateAgent(agentId);
      await onChanged?.();
    } catch (error) {
      setFailure(describeError(error, t("无法更新 Agent")).message);
    } finally {
      setUpdating(false);
    }
  };

  return (
    <div className="agent-manage-row" data-accent={agentAccent(agentId)} data-testid={`agent-${agentId}`}>
      <div className="agent-manage-summary">
        <div className="agent-manage-identity">
          <span className="agent-icon" title={agentTagline(agentId, t) || undefined}>
            <AgentIcon agentId={agentId} size={20} />
          </span>
          <span className="agent-manage-identity-copy">
            <strong>{catalog?.name || agentId}</strong>
            {failure ? <small className="agent-manage-note is-error">{failure}</small> : null}
          </span>
        </div>
        <div className="agent-manage-meta" aria-label={t("状态")}>
          {providerName ? (
            <span className="agent-manage-pill" title={t("模型服务")}>
              <i aria-hidden="true" />
              {providerName}
            </span>
          ) : null}
          <span className={`agent-manage-pill${profileLabel ? "" : " is-muted"}`} title={t("配置模板")}>
            {profileLabel || t("未绑定")}
          </span>
          {model ? (
            <span className="agent-manage-pill agent-manage-model" title={t("模型")}>
              {model}
            </span>
          ) : null}
          {version ? (
            <span className={`agent-manage-pill agent-manage-version${version.behind ? " is-behind" : ""}`} title={t("版本")}>
              {version.text}
            </span>
          ) : null}
          {statusLabel ? <span className={`agent-manage-state${failure ? " is-error" : ""}`}>{statusLabel}</span> : null}
        </div>
      </div>
      <div className="agent-manage-actions">
        {!canLaunch ? (
          <Link
            className="button button-secondary"
            to={`/agents/${agentId}`}
            title={t("编辑这个 Agent 关联的 Profile")}
          >
            {t("配置")}
          </Link>
        ) : null}
        {npmAgents.has(agentId) ? (
          <button className="button button-secondary" type="button" onClick={() => void update()} disabled={updating || launching} title={t("执行 npm update")}>
            {t("更新")}
          </button>
        ) : null}
        {canLaunch ? (
          <button
            className="button button-primary"
            type="button"
            onClick={() => void launch()}
            disabled={launching}
            title={t("在新终端窗口中启动，并载入 OneAgent 写入的配置")}
          >
            {launching ? <RefreshCw size={14} className="spin" aria-hidden="true" /> : null}
            {t("启动")}
          </button>
        ) : null}
      </div>
      <details className="agent-manage-details">
        <summary>{t("详情")}</summary>
        <div className="agent-manage-details-body">
          {canLaunch ? (
            <Link
              className="agent-manage-detail-action"
              to={`/agents/${agentId}`}
              title={t("编辑这个 Agent 关联的 Profile")}
            >
              {t("配置")}
            </Link>
          ) : null}
          {providerName ? (
            <div><small>{t("模型服务")}</small><span>{providerName}</span></div>
          ) : null}
          {profileLabel ? (
            <div><small>{t("配置模板")}</small><span>{profileLabel}</span></div>
          ) : null}
          {model ? (
            <div><small>{t("模型")}</small><span className="agent-manage-detail-code">{model}</span></div>
          ) : null}
          {baseUrl ? (
            <div><small>URL</small><span className="agent-manage-detail-code" title={baseUrl}>{baseUrl}</span></div>
          ) : null}
          {target.text && target.text !== t("未配置") ? (
            <div><small>{t("检测状态")}</small><span>{target.text}</span></div>
          ) : null}
          {target.note ? (
            <div className="agent-manage-detail-note"><small>{t("备注")}</small><span>{target.note}</span></div>
          ) : null}
          {npmPackages[agentId] ? (
            <div><small>npm</small><span className="agent-manage-detail-code">{npmPackages[agentId]}</span></div>
          ) : null}
        </div>
      </details>
    </div>
  );
}
