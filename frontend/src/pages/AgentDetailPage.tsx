import { FlaskConical } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { AdvancedSection } from "../components/AdvancedSection";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { AgentIcon, agentTagline } from "../components/icons/agents";
import { AgentQuickSwitch } from "../components/AgentQuickSwitch";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { targetSummary, versionNote } from "../components/AgentManageRow";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { PROTOCOL_LABELS } from "../types/api";
import type { ProbeResponse, ProviderId } from "../types/api";

export function AgentDetailPage() {
  const { agentId = "" } = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { locale, t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const agent = status?.agents[agentId];
  const catalog = status?.catalog.find((item) => item.id === agentId);

  const initialProvider = agent?.provider && status?.providers[agent.provider] ? agent.provider : "ppio";
  const [provider, setProvider] = useState<ProviderId>(initialProvider);
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState(agent?.model || "");
  const [smallFastModel, setSmallFastModel] = useState("");
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probeState, setProbeState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState<{ restart: string; next: string } | null>(null);
  const [failure, setFailure] = useState("");
  // SecureKeyField echoes from its own state by design, so clearing the value
  // cannot clear the field; remounting it does.
  const [keyFieldId, setKeyFieldId] = useState(0);

  useEffect(() => {
    if (!status?.providers[provider]?.has_key) return;
    let active = true;
    void api.getProvider(provider)
      .then((entry) => {
        if (active) setApiKey(entry.api_key);
      })
      .catch((error) => {
        if (active) setFailure(describeError(error, t("无法读取已保存的 API Key")).message);
      });
    return () => { active = false; };
  }, [provider, status?.providers]);

  if (!status || !agent || !catalog || catalog.configMode !== "auto") {
    return (
      <PageScaffold
        title="Agent"
        primaryLabel={t("返回总览")}
        onPrimary={() => navigate("/overview")}
      >
        <div className="empty-overview">
          <strong>{t("找不到可配置的 Agent")}</strong>
          <span>{agentId ? t("{id} 不在可一键配置的范围内。", { id: agentId }) : t("未指定 Agent。")}</span>
        </div>
      </PageScaffold>
    );
  }

  const version = versionNote(agent, t);
  const target = targetSummary(agent, status.providers, t);
  // A configuration OneAgent did not write is the case worth warning about:
  // applying replaces it, and the user may not know it is there. A backup is
  // taken either way, but saying so beforehand is the point.
  const willOverwrite =
    agent.detected && !agent.detected.managedByOneAgent && !agent.detected.unreadable
      ? agent.detected
      : null;
  const canProbe = Boolean(apiKey);
  // ActivateAgent resolves the key from the request, then the Profile secret,
  // then the Provider's stored one (internal/app/agent.go:92-102), so a typed key
  // is not the only way to have one. Requiring it here meant a user who only
  // wanted to change the model had to paste their key again.
  const providerHasKey = Boolean(status.providers[provider]?.has_key);
  const hasKeySomewhere = Boolean(apiKey) || providerHasKey;
  // A probe is a check, not a gate: the backend never asks for one. Applying
  // without it is allowed and warned about rather than blocked.
  const canApply = hasKeySomewhere && probeState !== "loading" && !applying;
  const untested = probeState !== "success";

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
        apiBaseUrl: "",
        apiKey,
        model,
        agents: [agentId],
      });
      setProbe(result);
      setProbeState(result.ok ? "success" : "error");
    } catch (error) {
      setProbeState("error");
      setFailure(describeError(error, t("连接测试失败")).message);
    }
  };

  const apply = async () => {
    setApplying(true);
    setFailure("");
    try {
      const result = await api.activateAgent(agentId, {
        provider,
        apiBaseUrl: "",
        apiKey,
        model,
        profileId: params.get("profile") || undefined,
        smallFastModel,
      });
      setApplied({ restart: result.restart, next: result.next });
      setApiKey("");
      setKeyFieldId((value) => value + 1);
      setProbeState("idle");
      setProbe(null);
      void refreshStatus();
    } catch (error) {
      setFailure(describeError(error, t("应用配置失败")).message);
    } finally {
      setApplying(false);
    }
  };

  return (
    <PageScaffold
      title={catalog.name}
      description={agentTagline(agentId, t) || undefined}
      backLabel={t("返回总览")}
      onBack={() => navigate("/overview")}
      primaryLabel={applying ? t("应用中") : t("应用")}
      onPrimary={() => void apply()}
      primaryDisabled={!canApply}
    >
      <div className="detail-head">
        <span className="agent-icon">
          <AgentIcon agentId={agentId} size={20} />
        </span>
        <dl className="detail-facts">
          <div>
            <dt>{t("当前指向")}</dt>
            <dd>
              {target.text}
              {target.note ? <small className="detail-drift">{target.note}</small> : null}
            </dd>
          </div>
          <div>
            <dt>{t("版本")}</dt>
            <dd className={version?.behind ? "is-behind" : ""}>{version?.text || t("未安装")}</dd>
          </div>
          <div>
            <dt>{t("配置文件")}</dt>
            <dd>{agent.config || "—"}</dd>
          </div>
          <div>
            <dt>{t("备份")}</dt>
            <dd>{status.backups[agentId] ? t("已有历史备份") : t("暂无")}</dd>
          </div>
        </dl>
      </div>

      {/* Above the form: switching between existing Profiles is the common case,
          and editing fields by hand is the fallback for when none of them fit. */}
      <AgentQuickSwitch
        agentId={agentId}
        status={agent}
        profiles={status.profiles}
        providers={status.providers}
        onSwitched={() => refreshStatus()}
      />

      <section className="detail-form">
        {willOverwrite ? (
          <div className="notice notice-warning">
            <strong>{t("这个 Agent 已有配置，不是 OneAgent 写入的")}</strong>
            <span>
              {t("当前指向 {target}。应用后会被替换，原文件会先备份到同目录的", {
                target: [willOverwrite.baseUrl || t("未知端点"), willOverwrite.model].filter(Boolean).join(" · "),
              })}
              {" "}
              <code>*.backup-&lt;{t("时间戳")}&gt;</code>{locale === "en" ? "." : "。"}
            </span>
          </div>
        ) : null}
        <ProviderSegment
          value={provider}
          providers={status.providers}
          onAdd={() => navigate(`/providers/new?returnTo=${encodeURIComponent(`/agents/${agentId}`)}`)}
          onChange={(next) => {
            setProvider(next);
            setApiKey("");
            resetVerdict();
          }}
        />
        <SecureKeyField
          key={keyFieldId}
          value={apiKey}
          onChange={(value) => {
            setApiKey(value);
            resetVerdict();
          }}
        />

        <div className="connection-row">
          <button
            className="button button-secondary"
            type="button"
            onClick={() => void testConnection()}
            disabled={!canProbe || probeState === "loading"}
          >
            <FlaskConical size={16} />
            {t("测试连接")}
          </button>
          <ConnectionStatus state={probeState} result={probe} />
        </div>
        {catalog.protocol ? (
          <small className="detail-protocol">{t("将测试 {protocol} 协议", { protocol: PROTOCOL_LABELS[catalog.protocol] })}</small>
        ) : null}
        {/* Say where the key is coming from, and that a skipped test is allowed.
            Without this the empty field reads as "no key" on a Provider that has
            one, and the enabled Apply button looks like a bug. */}
        {!apiKey && providerHasKey ? (
          <small className="detail-protocol">{t("留空则使用该 Provider 已保存的 Key。")}</small>
        ) : null}
        {canApply && untested ? (
          <small className="detail-protocol">{t("未测试连接也可以应用，配置文件会先备份。")}</small>
        ) : null}

        <AdvancedSection hint={t("可以指定具体模型。留空时由端点的模型列表自动选择，多数情况保持默认即可。")}>
          <div className="field-stack">
            <label htmlFor="detail-model">{t("模型")}</label>
            <input
              id="detail-model"
              className="text-field"
              value={model}
              onChange={(event) => {
                setModel(event.target.value);
                resetVerdict();
              }}
              placeholder={t("留空则由端点的模型列表自动选择")}
            />
          </div>
          {agentId === "claude-code" ? (
            <div className="field-stack">
              <label htmlFor="detail-small-fast-model">{t("快速小模型")}</label>
              <input
                id="detail-small-fast-model"
                className="text-field"
                value={smallFastModel}
                onChange={(event) => setSmallFastModel(event.target.value)}
                placeholder={t("留空则与主模型相同")}
              />
            </div>
          ) : null}
        </AdvancedSection>

        {failure ? <p className="agent-manage-error">{failure}</p> : null}
        {applied ? (
          <div className="agent-manage-applied">
            <strong>{t("已写入配置")}</strong>
            <span>{applied.restart}</span>
            {applied.next ? <pre>{applied.next}</pre> : null}
          </div>
        ) : null}
      </section>
    </PageScaffold>
  );
}
