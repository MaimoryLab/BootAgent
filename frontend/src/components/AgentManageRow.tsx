import { ChevronDown, FlaskConical, Settings2 } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../api/client";
import { PROTOCOL_LABELS } from "../types/api";
import type { AgentCatalogItem, AgentStatus, ProbeResponse, ProviderId, StatusResponse } from "../types/api";
import { ConnectionStatus } from "./ConnectionStatus";
import { ProviderSegment } from "./ProviderSegment";
import { SecureKeyField } from "./SecureKeyField";
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

function versionNote(status: AgentStatus): { text: string; behind: boolean } | null {
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
    // The arrow already says which way this goes; "可更新" was a third word
    // restating it.
    return { text: `${status.version} → ${status.lockedVersion}`, behind: true };
  }
  return { text: `${status.version}（锁定 ${status.lockedVersion}）`, behind: false };
}

export function AgentManageRow({
  agentId,
  catalog,
  status,
  providers,
  onActivated,
}: {
  agentId: string;
  catalog: AgentCatalogItem | undefined;
  status: AgentStatus;
  providers: Providers;
  onActivated: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [provider, setProvider] = useState<ProviderId>((status.provider as ProviderId) || "ppio");
  const [customBaseUrl, setCustomBaseUrl] = useState(provider === "custom" ? status.baseUrl || "" : "");
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState(status.model || "");
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probeState, setProbeState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState<{ restart: string; next: string } | null>(null);
  const [failure, setFailure] = useState("");
  // SecureKeyField echoes from its own state by design, so clearing the key
  // here cannot clear the field. Remounting it does, which matters because a
  // key left in a visible form outlives the request that needed it.
  const [keyFieldId, setKeyFieldId] = useState(0);

  const version = versionNote(status);
  const canProbe = Boolean(apiKey) && (provider !== "custom" || Boolean(customBaseUrl.trim()));
  // Applying requires a passing probe for the same reason the wizard does: a
  // rejected key would otherwise be written into a config that then looks fine.
  const canApply = canProbe && probeState === "success" && !applying;

  const resetVerdict = () => {
    setProbe(null);
    setProbeState("idle");
    setApplied(null);
    setFailure("");
  };

  const testConnection = async () => {
    setProbeState("loading");
    setFailure("");
    try {
      const result = await api.probe({
        provider,
        apiBaseUrl: provider === "custom" ? customBaseUrl : "",
        apiKey,
        model,
        agents: [agentId],
      });
      setProbe(result);
      setProbeState(result.ok ? "success" : "error");
    } catch (error) {
      setProbeState("error");
      setFailure(describeError(error, "连接测试失败").message);
    }
  };

  const apply = async () => {
    setApplying(true);
    setFailure("");
    try {
      const result = await api.activateAgent(agentId, {
        provider,
        apiBaseUrl: provider === "custom" ? customBaseUrl : "",
        apiKey,
        model,
      });
      setApplied({ restart: result.restart, next: result.next });
      setApiKey("");
      setKeyFieldId((value) => value + 1);
      setProbeState("idle");
      setProbe(null);
      onActivated();
    } catch (error) {
      setFailure(describeError(error, "应用配置失败").message);
    } finally {
      setApplying(false);
    }
  };

  const actionLabel = !status.installed ? "安装并配置" : status.configured ? "改配置" : "配置";

  return (
    <div className={`agent-manage-row${open ? " is-open" : ""}`}>
      <div className="agent-manage-head">
        <div className="agent-manage-identity">
          <strong>{catalog?.name || agentId}</strong>
          <span className="agent-manage-target">
            {status.provider && status.model ? (
              <>
                {providers[status.provider]?.name || status.provider}
                <span aria-hidden="true"> · </span>
                {status.model}
              </>
            ) : (
              "未配置"
            )}
          </span>
          {version ? (
            <small className={version.behind ? "is-behind" : ""}>{version.text}</small>
          ) : null}
        </div>
        <div className="agent-manage-actions">
          <StatusBadge tone={status.installed ? "success" : "warning"}>
            {status.installed ? "已安装" : "待安装"}
          </StatusBadge>
          <button
            className="button button-secondary"
            type="button"
            aria-expanded={open}
            onClick={() => {
              setOpen((value) => !value);
              resetVerdict();
            }}
          >
            <Settings2 size={15} />
            {actionLabel}
          </button>
        </div>
      </div>

      {open ? (
        <div className="agent-manage-panel">
          <ProviderSegment
            value={provider}
            onChange={(next) => {
              setProvider(next);
              resetVerdict();
            }}
          />
          {provider === "custom" ? (
            <div className="field-stack">
              <label htmlFor={`${agentId}-base-url`}>Base URL</label>
              <input
                id={`${agentId}-base-url`}
                className="text-field"
                value={customBaseUrl}
                onChange={(event) => {
                  setCustomBaseUrl(event.target.value);
                  resetVerdict();
                }}
                placeholder="https://models.example.com/openai"
                inputMode="url"
              />
            </div>
          ) : null}
          <div className="field-stack">
            <label htmlFor={`${agentId}-model`}>模型</label>
            <input
              id={`${agentId}-model`}
              className="text-field"
              value={model}
              onChange={(event) => {
                setModel(event.target.value);
                resetVerdict();
              }}
              placeholder="留空则由端点的模型列表自动选择"
            />
          </div>
          <SecureKeyField
            key={keyFieldId}
            value={apiKey}
            onChange={(value) => {
              setApiKey(value);
              // Any key edit invalidates the previous verdict; keeping a stale
              // success would let a wrong key through.
              resetVerdict();
            }}
          />
          {catalog?.protocol ? (
            <small className="agent-manage-protocol">
              将测试 {PROTOCOL_LABELS[catalog.protocol]} 协议
            </small>
          ) : null}

          <div className="connection-row">
            <button
              className="button button-secondary"
              type="button"
              onClick={() => void testConnection()}
              disabled={!canProbe || probeState === "loading"}
            >
              <FlaskConical size={16} />
              测试连接
            </button>
            <button className="button button-primary" type="button" onClick={() => void apply()} disabled={!canApply}>
              {applying ? "应用中" : "应用"}
            </button>
            <ConnectionStatus state={probeState} result={probe} />
          </div>

          {failure ? <p className="agent-manage-error">{failure}</p> : null}
          {applied ? (
            <div className="agent-manage-applied">
              <strong>已写入配置</strong>
              {/* Agents read their config at startup, so the switch is invisible
                  until the process restarts. */}
              <span>{applied.restart}</span>
              {applied.next ? <pre>{applied.next}</pre> : null}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
