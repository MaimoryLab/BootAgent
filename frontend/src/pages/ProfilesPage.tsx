import { KeyRound, Layers, Pencil, Play, Plus, Save, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { AgentRow } from "../components/AgentRow";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { SecureKeyField } from "../components/SecureKeyField";
import { useWizard } from "../state/WizardContext";
import type { ProfileSummary, ProviderId } from "../types/api";

interface ProfileDraft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
  agentIds: string[];
  apiKey: string;
  hasKey: boolean;
  originalId: string;
  originalProvider: ProviderId;
}

function editDraft(profile: ProfileSummary): ProfileDraft {
  return {
    id: profile.id,
    label: profile.label,
    provider: profile.provider,
    model: profile.model || "",
    agentIds: [...profile.agentIds],
    apiKey: "",
    hasKey: profile.hasKey,
    originalId: profile.id,
    originalProvider: profile.provider,
  };
}

export function ProfilesPage() {
  const navigate = useNavigate();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [editor, setEditor] = useState<ProfileDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState("");
  const [failure, setFailure] = useState("");
  const [success, setSuccess] = useState("");

  if (!status) {
    return (
      <PageScaffold title="配置模板">
        <div className="loading-block"><span className="spinner" />正在读取环境状态</div>
      </PageScaffold>
    );
  }

  const profiles = status.profiles;
  const configurableAgents = status.catalog.filter((agent) => agent.configMode === "auto");
  const nameOf = (agentId: string) =>
    status.catalog.find((item) => item.id === agentId)?.name || agentId;
  const keepsSavedKey = Boolean(
    editor?.hasKey && editor.originalProvider === editor.provider && !editor.apiKey,
  );
  const providerHasKey = Boolean(editor && status.providers[editor.provider]?.has_key);
  const canSave = Boolean(
    editor?.id.trim() && editor.label.trim() && editor.model.trim() && editor.agentIds.length
      && (editor.apiKey || keepsSavedKey || providerHasKey),
  );

  const openNew = () => {
    const provider = status.providers.ppio ? "ppio" : Object.keys(status.providers)[0] || "";
    setEditor({
      id: "",
      label: "",
      provider,
      model: "",
      agentIds: [],
      apiKey: "",
      hasKey: false,
      originalId: "",
      originalProvider: provider,
    });
    setFailure("");
    setSuccess("");
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor || !canSave) return;
    setBusy(true);
    setFailure("");
    setSuccess("");
    try {
      await api.saveProfile({
        id: editor.id.trim(),
        label: editor.label.trim(),
        provider: editor.provider,
        apiBaseUrl: "",
        apiKey: editor.apiKey,
        model: editor.model.trim(),
        configMode: "provider",
        agentIds: editor.agentIds,
      });
      await refreshStatus();
      setEditor(null);
    } catch (error) {
      setFailure(describeError(error, "无法保存 Profile").message);
    } finally {
      setBusy(false);
    }
  };

  const apply = async (profile: ProfileSummary) => {
    if (!profile.model || !profile.agentIds.length) return;
    setApplying(profile.id);
    setFailure("");
    setSuccess("");
    try {
      const result = await api.install({
        agents: profile.agentIds,
        profile_agents: profile.agentIds,
        provider: profile.provider,
        api_base_url: profile.baseUrl || "",
        api_key: "",
        model: profile.model,
        profile_id: profile.id,
        configure: true,
        install_agent: true,
        locked_version: true,
        skip_test: false,
      });
      if (!result.ok) {
        throw new Error(result.results.find((item) => item.status === "failed")?.message || "应用 Profile 失败");
      }
      await refreshStatus();
      setSuccess(`${profile.label} 已应用到 ${profile.agentIds.length} 个 Agent`);
    } catch (error) {
      setFailure(describeError(error, "无法应用 Profile").message);
    } finally {
      setApplying("");
    }
  };

  return (
    <PageScaffold title="配置模板" description="在这里创建 Profile，再将它应用到所选 Agent。">
      <div className="profile-toolbar">
        <button className="button button-secondary" type="button" onClick={openNew}>
          <Plus size={15} />
          新增 Profile
        </button>
      </div>

      {editor ? (
        <form className="profile-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{editor.originalId ? `编辑 ${editor.label || editor.id}` : "新增 Profile"}</strong>
            <button className="icon-button" type="button" onClick={() => setEditor(null)} aria-label="关闭编辑" title="关闭编辑">
              <X size={16} />
            </button>
          </header>

          <div className="profile-editor-grid">
            <div className="field-stack">
              <label htmlFor="profile-id">Profile ID</label>
              <input
                id="profile-id"
                value={editor.id}
                onChange={(event) => setEditor({ ...editor, id: event.target.value })}
                pattern="[a-z0-9][a-z0-9_-]{0,63}"
                placeholder="例如 team-ppio"
                disabled={Boolean(editor.originalId)}
                required
              />
            </div>
            <div className="field-stack">
              <label htmlFor="profile-label">名称</label>
              <input id="profile-label" value={editor.label} onChange={(event) => setEditor({ ...editor, label: event.target.value })} placeholder="例如 团队 PPIO" required />
            </div>
            <div className="profile-editor-wide">
              <ProviderSegment
                value={editor.provider}
                providers={status.providers}
                onAdd={() => navigate(`/providers/new?returnTo=${encodeURIComponent("/profiles")}`)}
                onChange={(provider) => setEditor({ ...editor, provider, apiKey: "" })}
              />
            </div>
            <div className="field-stack profile-editor-wide">
              <label htmlFor="profile-model">模型</label>
              <input id="profile-model" value={editor.model} onChange={(event) => setEditor({ ...editor, model: event.target.value })} placeholder="例如 deepseek/deepseek-v3" required />
            </div>
            <div className="profile-editor-wide">
              <SecureKeyField value={editor.apiKey} onChange={(apiKey) => setEditor({ ...editor, apiKey })} />
              {keepsSavedKey ? <p className="profile-key-hint">留空将保留这个 Profile 已保存的 Key。</p> : null}
              {!keepsSavedKey && !editor.apiKey && providerHasKey ? <p className="profile-key-hint">留空将使用 Provider 已保存的 Key。</p> : null}
            </div>
            <fieldset className="profile-agent-picker profile-editor-wide">
              <legend>适用 Agent</legend>
              <div className="agent-list">
                {configurableAgents.map((agent) => (
                  <AgentRow
                    key={agent.id}
                    agent={agent}
                    status={status.agents[agent.id]}
                    selected={editor.agentIds.includes(agent.id)}
                    onToggle={() => setEditor({
                      ...editor,
                      agentIds: editor.agentIds.includes(agent.id)
                        ? editor.agentIds.filter((id) => id !== agent.id)
                        : [...editor.agentIds, agent.id],
                    })}
                  />
                ))}
              </div>
            </fieldset>
          </div>

          <footer>
            <button className="button button-secondary" type="button" onClick={() => setEditor(null)}>取消</button>
            <button className="button button-primary" type="submit" disabled={!canSave || busy}>
              <Save size={15} />
              {busy ? "保存中" : "保存 Profile"}
            </button>
          </footer>
        </form>
      ) : null}

      {failure ? <p className="agent-manage-error">{failure}</p> : null}
      {success ? <div className="agent-manage-applied"><strong>应用完成</strong><span>{success}</span></div> : null}

      {!profiles.length && !editor ? (
        <div className="empty-overview">
          <Layers size={26} />
          <strong>还没有 Profile</strong>
          <span>新建一个 Profile，保存 Provider、模型、Key 和适用 Agent。</span>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((profile) => {
            const canApply = Boolean(
              profile.model && profile.agentIds.length
                && (profile.hasKey || status.providers[profile.provider]?.has_key),
            );
            return (
              <article className="profile-card" key={profile.id} data-testid={`profile-${profile.id}`}>
                <header>
                  <span className="profile-title"><strong>{profile.label}</strong><small>{profile.id}</small></span>
                  <span className="profile-card-actions">
                    <span className={`profile-key${profile.hasKey ? " has-key" : ""}`}>
                      <KeyRound size={12} aria-hidden="true" />
                      {profile.hasKey ? "已保存密钥" : "未保存密钥"}
                    </span>
                    <button className="icon-button" type="button" onClick={() => { setEditor(editDraft(profile)); setFailure(""); setSuccess(""); }} aria-label={`编辑 ${profile.label}`} title="编辑">
                      <Pencil size={14} />
                    </button>
                  </span>
                </header>
                <p className="profile-target">
                  {status.providers[profile.provider]?.name || profile.provider}
                  <span aria-hidden="true"> · </span>
                  {profile.model || "未指定模型"}
                </p>
                <p className="profile-agents">
                  适用：{profile.agentIds.length ? profile.agentIds.map(nameOf).join("、") : "未选择 Agent"}
                </p>
                <footer>
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => void apply(profile)}
                    disabled={!canApply || Boolean(applying)}
                    title={canApply ? "安装缺失的 Agent 并应用此 Profile" : "请先补全模型、Agent 和 Key"}
                  >
                    <Play size={14} />
                    {applying === profile.id ? "应用中" : "应用到 Agent"}
                  </button>
                </footer>
              </article>
            );
          })}
        </div>
      )}
    </PageScaffold>
  );
}
