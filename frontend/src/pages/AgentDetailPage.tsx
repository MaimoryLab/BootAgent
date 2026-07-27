import { FlaskConical } from "lucide-react";
import { useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import { api, describeError } from "../api/client";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { AgentIcon, agentTagline } from "../components/icons/agents";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { versionNote } from "../components/AgentManageRow";
import { useWizard } from "../state/WizardContext";
import { PROTOCOL_LABELS } from "../types/api";
import type { ProbeResponse, ProviderId } from "../types/api";

export function AgentDetailPage() {
  const { agentId = "" } = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const agent = status?.agents[agentId];
  const catalog = status?.catalog.find((item) => item.id === agentId);

  const [provider, setProvider] = useState<ProviderId>((agent?.provider as ProviderId) || "ppio");
  const [customBaseUrl, setCustomBaseUrl] = useState(agent?.provider === "custom" ? agent.baseUrl || "" : "");
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState(agent?.model || "");
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probeState, setProbeState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState<{ restart: string; next: string } | null>(null);
  const [failure, setFailure] = useState("");
  // SecureKeyField echoes from its own state by design, so clearing the value
  // cannot clear the field; remounting it does.
  const [keyFieldId, setKeyFieldId] = useState(0);

  if (!status || !agent || !catalog || catalog.configMode !== "auto") {
    return (
      <PageScaffold
        title="Agent"
        primaryLabel="返回总览"
        onPrimary={() => navigate("/overview")}
      >
        <div className="empty-overview">
          <strong>找不到可配置的 Agent</strong>
          <span>{agentId ? `${agentId} 不在可一键配置的范围内。` : "未指定 Agent。"}</span>
        </div>
      </PageScaffold>
    );
  }

  const version = versionNote(agent);
  const canProbe = Boolean(apiKey) && (provider !== "custom" || Boolean(customBaseUrl.trim()));
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
        profileId: params.get("profile") || undefined,
      });
      setApplied({ restart: result.restart, next: result.next });
      setApiKey("");
      setKeyFieldId((value) => value + 1);
      setProbeState("idle");
      setProbe(null);
      void refreshStatus();
    } catch (error) {
      setFailure(describeError(error, "应用配置失败").message);
    } finally {
      setApplying(false);
    }
  };

  return (
    <PageScaffold
      title={catalog.name}
      description={agentTagline(agentId) || undefined}
      backLabel="返回总览"
      onBack={() => navigate("/overview")}
      primaryLabel={applying ? "应用中" : "应用"}
      onPrimary={() => void apply()}
      primaryDisabled={!canApply}
    >
      <div className="detail-head">
        <span className="agent-icon">
          <AgentIcon agentId={agentId} size={20} />
        </span>
        <dl className="detail-facts">
          <div>
            <dt>当前指向</dt>
            <dd>
              {agent.provider && agent.model
                ? `${status.providers[agent.provider]?.name || agent.provider} · ${agent.model}`
                : "未配置"}
            </dd>
          </div>
          <div>
            <dt>版本</dt>
            <dd className={version?.behind ? "is-behind" : ""}>{version?.text || "未安装"}</dd>
          </div>
          <div>
            <dt>配置文件</dt>
            <dd>{agent.config || "—"}</dd>
          </div>
          <div>
            <dt>备份</dt>
            <dd>{status.backups[agentId] ? "已有历史备份" : "暂无"}</dd>
          </div>
        </dl>
      </div>

      <section className="detail-form">
        <ProviderSegment
          value={provider}
          onChange={(next) => {
            setProvider(next);
            resetVerdict();
          }}
        />
        {provider === "custom" ? (
          <div className="field-stack">
            <label htmlFor="detail-base-url">Base URL</label>
            <input
              id="detail-base-url"
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
          <label htmlFor="detail-model">模型</label>
          <input
            id="detail-model"
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
            测试连接
          </button>
          <ConnectionStatus state={probeState} result={probe} />
        </div>
        {catalog.protocol ? (
          <small className="detail-protocol">将测试 {PROTOCOL_LABELS[catalog.protocol]} 协议</small>
        ) : null}

        {failure ? <p className="agent-manage-error">{failure}</p> : null}
        {applied ? (
          <div className="agent-manage-applied">
            <strong>已写入配置</strong>
            <span>{applied.restart}</span>
            {applied.next ? <pre>{applied.next}</pre> : null}
          </div>
        ) : null}
      </section>
    </PageScaffold>
  );
}
