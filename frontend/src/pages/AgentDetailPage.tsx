import { FlaskConical } from "lucide-react";
import { useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import { api, describeError } from "../api/client";
import { AdvancedSection } from "../components/AdvancedSection";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { AgentIcon, agentTagline } from "../components/icons/agents";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { targetSummary, versionNote } from "../components/AgentManageRow";
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
  // A ref, not state: the key must not enter React state, and the field it comes
  // from is uncontrolled so the DOM node holds the only copy. hasKey drives the
  // buttons that need to know whether something was typed.
  const apiKeyRef = useRef("");
  const [hasKey, setHasKey] = useState(false);
  const keyFieldRef = useRef<HTMLInputElement | null>(null);
  const [model, setModel] = useState(agent?.model || "");
  const [smallFastModel, setSmallFastModel] = useState("");
  const [probe, setProbe] = useState<ProbeResponse | null>(null);
  const [probeState, setProbeState] = useState<"idle" | "loading" | "success" | "error">("idle");
  const [applying, setApplying] = useState(false);
  const [applied, setApplied] = useState<{ restart: string; next: string } | null>(null);
  const [failure, setFailure] = useState("");


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
  const target = targetSummary(agent, status.providers);
  // A configuration OneAgent did not write is the case worth warning about:
  // applying replaces it, and the user may not know it is there. A backup is
  // taken either way, but saying so beforehand is the point.
  const willOverwrite =
    agent.detected && !agent.detected.managedByOneAgent && !agent.detected.unreadable
      ? agent.detected
      : null;
  const canProbe = hasKey && (provider !== "custom" || Boolean(customBaseUrl.trim()));
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
        apiKey: apiKeyRef.current,
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
        apiKey: apiKeyRef.current,
        model,
        profileId: params.get("profile") || undefined,
        smallFastModel,
      });
      setApplied({ restart: result.restart, next: result.next });
      // Clearing the ref is not enough: the characters are in the DOM node, so
      // the field itself has to be emptied or the key stays on screen.
      apiKeyRef.current = "";
      if (keyFieldRef.current) {
        keyFieldRef.current.value = "";
      }
      setHasKey(false);
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
              {target.text}
              {target.note ? <small className="detail-drift">{target.note}</small> : null}
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
        {willOverwrite ? (
          <div className="notice notice-warning">
            <strong>这个 Agent 已有配置，不是 OneAgent 写入的</strong>
            <span>
              当前指向 {willOverwrite.baseUrl || "未知端点"}
              {willOverwrite.model ? ` · ${willOverwrite.model}` : ""}。应用后会被替换，原文件会先备份到同目录的
              {" "}
              <code>*.backup-&lt;时间戳&gt;</code>。
            </span>
          </div>
        ) : null}
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
        <SecureKeyField
          onChange={(value) => {
            apiKeyRef.current = value;
            setHasKey(Boolean(value));
            resetVerdict();
          }}
          register={(node) => {
            keyFieldRef.current = node;
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

        <AdvancedSection hint="可以指定具体模型。留空时由端点的模型列表自动选择，多数情况保持默认即可。">
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
          {agentId === "claude-code" ? (
            <div className="field-stack">
              <label htmlFor="detail-small-fast-model">快速小模型</label>
              <input
                id="detail-small-fast-model"
                className="text-field"
                value={smallFastModel}
                onChange={(event) => setSmallFastModel(event.target.value)}
                placeholder="留空则与主模型相同"
              />
            </div>
          ) : null}
        </AdvancedSection>

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
